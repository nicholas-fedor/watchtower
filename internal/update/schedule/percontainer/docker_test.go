package percontainer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// fakeStream hands out one connection per Events call; the test drives each.
type fakeStream struct {
	mu          sync.Mutex
	connections chan *connection
	options     []dockerClient.EventsListOptions
}

type connection struct {
	messages chan events.Message
	errs     chan error
}

func newFakeStream() *fakeStream {
	return &fakeStream{connections: make(chan *connection, 8)}
}

func (f *fakeStream) Events(_ context.Context, options dockerClient.EventsListOptions) dockerClient.EventsResult {
	f.mu.Lock()
	f.options = append(f.options, options)
	f.mu.Unlock()

	c := &connection{messages: make(chan events.Message), errs: make(chan error, 1)}
	f.connections <- c

	return dockerClient.EventsResult{Messages: c.messages, Err: c.errs}
}

func (f *fakeStream) connected(t *testing.T) *connection {
	t.Helper()

	select {
	case c := <-f.connections:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("the source did not connect to the event stream")

		return nil
	}
}

// recorder is a Sink keeping the latest view.
type recorder struct {
	mu     sync.Mutex
	view   map[string]string
	resets int
}

func newRecorder() *recorder { return &recorder{view: map[string]string{}} }

func (r *recorder) Reset(schedules map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.resets++
	r.view = map[string]string{}

	maps.Copy(r.view, schedules)
}

func (r *recorder) Set(name, spec string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if spec == "" {
		delete(r.view, name)

		return
	}

	r.view[name] = spec
}

func (r *recorder) Remove(name string) { r.Set(name, "") }

func (r *recorder) snapshot() (map[string]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	view := map[string]string{}
	maps.Copy(view, r.view)

	return view, r.resets
}

func (r *recorder) waitFor(t *testing.T, want map[string]string, resets int) {
	t.Helper()

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		view, n := r.snapshot()
		assert.Equal(c, want, view)
		assert.GreaterOrEqual(c, n, resets)
	}, 5*time.Second, 10*time.Millisecond)
}

func containerEvent(action events.Action, attrs map[string]string) events.Message {
	return events.Message{Type: events.ContainerEventType, Action: action, Actor: events.Actor{ID: "id", Attributes: attrs}}
}

func send(t *testing.T, c *connection, msg events.Message) {
	t.Helper()

	select {
	case c.messages <- msg:
	case <-time.After(5 * time.Second):
		t.Fatal("the source stopped reading events")
	}
}

func startSource(t *testing.T, s *DockerSource) (*recorder, func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	sink := newRecorder()
	done := make(chan error, 1)

	go func() { done <- s.Run(ctx, sink) }()

	return sink, func() {
		cancel()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("the source did not stop")
		}
	}
}

func lister(containers ...types.FilterableContainer) Lister {
	return func(context.Context) ([]types.FilterableContainer, error) { return containers, nil }
}

func TestDockerSourceFollowsEvents(t *testing.T) {
	t.Parallel()

	stream := newFakeStream()
	source := &DockerSource{
		Events: stream,
		List: lister(
			labeled("web", map[string]string{labels.Schedule: "@every 10m"}),
			labeled("db", nil),
		),
	}

	sink, stop := startSource(t, source)
	defer stop()

	conn := stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@every 10m"}, 1)

	send(t, conn, containerEvent(events.ActionCreate, map[string]string{
		"name": "online", "image": "example/online", labels.Schedule: "@every 10m",
	}))
	sink.waitFor(t, map[string]string{"web": "@every 10m", "online": "@every 10m"}, 1)

	// An update recreates a container: destroyed, then created under the same name.
	send(t, conn, containerEvent(events.ActionDestroy, map[string]string{"name": "web", labels.Schedule: "@every 10m"}))
	sink.waitFor(t, map[string]string{"online": "@every 10m"}, 1)

	send(t, conn, containerEvent(events.ActionCreate, map[string]string{"name": "web", labels.Schedule: "@hourly"}))
	sink.waitFor(t, map[string]string{"web": "@hourly", "online": "@every 10m"}, 1)

	send(t, conn, containerEvent(events.ActionRename, map[string]string{
		"name": "online-2", "oldName": "/online", labels.Schedule: "@every 10m",
	}))
	sink.waitFor(t, map[string]string{"web": "@hourly", "online-2": "@every 10m"}, 1)

	// Events the source doesn't follow change nothing.
	send(t, conn, containerEvent(events.ActionStart, map[string]string{"name": "db", labels.Schedule: "@daily"}))
	sink.waitFor(t, map[string]string{"web": "@hourly", "online-2": "@every 10m"}, 1)

	stream.mu.Lock()
	options := stream.options[0]
	stream.mu.Unlock()

	assert.Equal(t, map[string]bool{"container": true}, options.Filters["type"])
	assert.Equal(t, map[string]bool{"create": true, "destroy": true, "rename": true}, options.Filters["event"])
}

func TestDockerSourceAppliesTheFilter(t *testing.T) {
	t.Parallel()

	inScope := func(c types.FilterableContainer) bool {
		scope, _ := c.Scope()

		return scope == "games"
	}

	stream := newFakeStream()
	source := &DockerSource{
		Events: stream,
		List: lister(
			labeled("web", map[string]string{labels.Schedule: "@every 10m", labels.Scope: "games"}),
			labeled("other", map[string]string{labels.Schedule: "@every 10m", labels.Scope: "media"}),
		),
		Filter: inScope,
	}

	sink, stop := startSource(t, source)
	defer stop()

	conn := stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@every 10m"}, 1)

	send(t, conn, containerEvent(events.ActionCreate, map[string]string{
		"name": "plex", labels.Schedule: "@hourly", labels.Scope: "media",
	}))
	send(t, conn, containerEvent(events.ActionCreate, map[string]string{
		"name": "online", labels.Schedule: "@hourly", labels.Scope: "games",
	}))
	sink.waitFor(t, map[string]string{"web": "@every 10m", "online": "@hourly"}, 1)
}

func TestDockerSourceReconnects(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		containers = []types.FilterableContainer{labeled("web", map[string]string{labels.Schedule: "@every 10m"})}
	)

	stream := newFakeStream()
	source := &DockerSource{
		Events: stream,
		List: func(context.Context) ([]types.FilterableContainer, error) {
			mu.Lock()
			defer mu.Unlock()

			return containers, nil
		},
		RetryDelay: time.Millisecond,
	}

	sink, stop := startSource(t, source)
	defer stop()

	first := stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@every 10m"}, 1)

	// While the stream is down a container changes: the next list finds it.
	mu.Lock()
	containers = []types.FilterableContainer{labeled("web", map[string]string{labels.Schedule: "@hourly"})}
	mu.Unlock()

	first.errs <- errors.New("daemon restarted")

	second := stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@hourly"}, 2)

	// A stream that just ends reconnects too.
	close(second.messages)

	stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@hourly"}, 3)
}

func TestDockerSourceRetriesAFailedList(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls int
	)

	stream := newFakeStream()
	source := &DockerSource{
		Events: stream,
		List: func(context.Context) ([]types.FilterableContainer, error) {
			mu.Lock()
			defer mu.Unlock()

			calls++
			if calls == 1 {
				return nil, errors.New("daemon busy")
			}

			return []types.FilterableContainer{labeled("web", map[string]string{labels.Schedule: "@daily"})}, nil
		},
		RetryDelay: time.Millisecond,
	}

	sink, stop := startSource(t, source)
	defer stop()

	stream.connected(t)
	stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@daily"}, 1)
}

func TestDockerSourceStopsWithItsContext(t *testing.T) {
	t.Parallel()

	stream := newFakeStream()
	source := &DockerSource{Events: stream, List: lister()}

	_, stop := startSource(t, source)

	stream.connected(t)
	stop()
}

func TestDockerSourceRefreshListsAgain(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		containers = []types.FilterableContainer{labeled("web", map[string]string{labels.Schedule: "@every 10m"})}
	)

	stream := newFakeStream()
	source := &DockerSource{
		Events: stream,
		List: func(context.Context) ([]types.FilterableContainer, error) {
			mu.Lock()
			defer mu.Unlock()

			return containers, nil
		},
	}

	sink, stop := startSource(t, source)
	defer stop()

	conn := stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@every 10m"}, 1)

	// A change the stream never reported (a connection that died silently):
	// a refresh finds it, on the same connection.
	mu.Lock()
	containers = []types.FilterableContainer{labeled("web", map[string]string{labels.Schedule: "@hourly"})}
	mu.Unlock()

	source.Refresh()
	source.Refresh() // (merged with the first)
	sink.waitFor(t, map[string]string{"web": "@hourly"}, 2)

	// Events keep flowing after it.
	send(t, conn, containerEvent(events.ActionCreate, map[string]string{"name": "online", labels.Schedule: "@daily"}))
	sink.waitFor(t, map[string]string{"web": "@hourly", "online": "@daily"}, 2)

	select {
	case <-stream.connections:
		t.Fatal("a refresh should not reconnect")
	default:
	}
}

func TestDockerSourceRefreshBeforeRunIsHarmless(t *testing.T) {
	t.Parallel()

	source := &DockerSource{Events: newFakeStream(), List: lister()}
	source.Refresh()
	source.Refresh()
}

// logLevels returns a logger and the levels of the lines it wrote.
func logLevels() (*zerolog.Logger, func() []string) {
	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)

	logger := zerolog.New(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()

		return buf.Write(p)
	})).Level(zerolog.DebugLevel)

	return &logger, func() []string {
		mu.Lock()
		defer mu.Unlock()

		var levels []string

		for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
			var entry struct {
				Level   string `json:"level"`
				Message string `json:"message"`
			}

			if json.Unmarshal([]byte(line), &entry) == nil && (strings.HasPrefix(entry.Message, "Lost Docker's") || strings.HasPrefix(entry.Message, "Reconnected to Docker's")) {
				levels = append(levels, entry.Level)
			}
		}

		return levels
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestDockerSourceLogsQuietlyUntilItKeepsFailing(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		failNext = 3
	)

	logger, levels := logLevels()
	stream := newFakeStream()
	source := &DockerSource{
		Events: stream,
		List: func(context.Context) ([]types.FilterableContainer, error) {
			mu.Lock()
			defer mu.Unlock()

			if failNext > 0 {
				failNext--

				return nil, errors.New("daemon starting")
			}

			return nil, nil
		},
		Log:        logger,
		RetryDelay: time.Millisecond,
	}

	_, stop := startSource(t, source)
	defer stop()

	// Three failed attempts, then a good connection.
	for range 4 {
		stream.connected(t)
	}

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, []string{"debug", "debug", "warn", "info"}, levels())
	}, 5*time.Second, 10*time.Millisecond, "one or two failures are routine; a third warns; recovering says so")
}
