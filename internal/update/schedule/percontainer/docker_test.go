package percontainer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
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

// newFakeStream returns a stream with no connections yet.
func newFakeStream() *fakeStream {
	return &fakeStream{connections: make(chan *connection, 8)}
}

// Events records the options and hands out a new connection for the test to drive.
func (f *fakeStream) Events(_ context.Context, options dockerClient.EventsListOptions) dockerClient.EventsResult {
	f.mu.Lock()
	f.options = append(f.options, options)
	f.mu.Unlock()

	c := &connection{messages: make(chan events.Message), errs: make(chan error, 1)}
	f.connections <- c

	return dockerClient.EventsResult{Messages: c.messages, Err: c.errs}
}

// connected waits for the source to subscribe and returns that connection.
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

// recorder is a Sink keeping the latest view (name to schedule) and the IDs
// it was given.
type recorder struct {
	mu      sync.Mutex
	view    map[string]string
	ids     map[string]string
	removed []string
	resets  int
}

// newRecorder returns an empty recorder.
func newRecorder() *recorder { return &recorder{view: map[string]string{}, ids: map[string]string{}} }

// Reset replaces the view with the snapshot and counts the reset.
func (r *recorder) Reset(containers map[string]Labeled) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.resets++
	r.view = map[string]string{}
	r.ids = map[string]string{}

	for name, labeled := range containers {
		r.view[name] = labeled.Spec
		r.ids[name] = labeled.ID
	}
}

// Set records a container's schedule and ID (an empty spec forgets it).
func (r *recorder) Set(name, id, spec string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if spec == "" {
		delete(r.view, name)
		delete(r.ids, name)

		return
	}

	r.view[name] = spec
	r.ids[name] = id
}

// Remove forgets a container.
func (r *recorder) Remove(name, containerID string) {
	r.mu.Lock()
	r.removed = append(r.removed, name+":"+containerID)
	r.mu.Unlock()

	r.Set(name, "", "")
}

// removals returns the removals asked for, as name:id.
func (r *recorder) removals() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.removed...)
}

// id returns the ID recorded for a container.
func (r *recorder) id(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.ids[name]
}

// snapshot returns a copy of the view and the number of resets so far.
func (r *recorder) snapshot() (map[string]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	view := map[string]string{}
	maps.Copy(view, r.view)

	return view, r.resets
}

// waitFor waits until the view is want, after at least resets resets.
func (r *recorder) waitFor(t *testing.T, want map[string]string, resets int) {
	t.Helper()

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		view, n := r.snapshot()
		assert.Equal(c, want, view)
		assert.GreaterOrEqual(c, n, resets)
	}, 5*time.Second, 10*time.Millisecond)
}

// containerEvent returns a container event with these attributes.
func containerEvent(action events.Action, attrs map[string]string) events.Message {
	return events.Message{Type: events.ContainerEventType, Action: action, Actor: events.Actor{ID: "id-" + attrs["name"], Attributes: attrs}}
}

// send delivers an event on a connection, failing if the source stops reading.
func send(t *testing.T, c *connection, msg events.Message) {
	t.Helper()

	select {
	case c.messages <- msg:
	case <-time.After(5 * time.Second):
		t.Fatal("the source stopped reading events")
	}
}

// startSource runs a source until the returned stop is called, recording what it reports.
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

// lister returns a Lister that always lists these containers.
func lister(containers ...types.FilterableContainer) Lister {
	return func(context.Context) ([]types.FilterableContainer, error) { return containers, nil }
}

// TestDockerSourceFollowsEvents verifies the list on connecting and each followed event: create, start, destroy, rename.
func TestDockerSourceFollowsEvents(t *testing.T) {
	t.Parallel()

	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		SettleDelay: -1,
		List: lister(
			labeled("web", map[string]string{labels.Schedule: "@every 10m"}),
			labeled("db", nil),
		),
	}

	sink, stop := startSource(t, source)
	defer stop()

	conn := stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@every 10m"}, 1)

	// (an opaque ID, as Docker's: nothing derived from the name)
	const onlineID = "7f3a9c2e"

	send(t, conn, events.Message{
		Type: events.ContainerEventType, Action: events.ActionCreate,
		Actor: events.Actor{ID: onlineID, Attributes: map[string]string{
			"name": "online", "image": "example/online", labels.Schedule: "@every 10m",
		}},
	})
	sink.waitFor(t, map[string]string{"web": "@every 10m", "online": "@every 10m"}, 1)

	// An update recreates a container: destroyed, then created under the same name.
	send(t, conn, containerEvent(events.ActionDestroy, map[string]string{"name": "web", labels.Schedule: "@every 10m"}))
	sink.waitFor(t, map[string]string{"online": "@every 10m"}, 1)

	send(t, conn, containerEvent(events.ActionCreate, map[string]string{"name": "web", labels.Schedule: "@hourly"}))
	sink.waitFor(t, map[string]string{"web": "@hourly", "online": "@every 10m"}, 1)

	// (a rename keeps the container's ID)
	send(t, conn, events.Message{
		Type: events.ContainerEventType, Action: events.ActionRename,
		Actor: events.Actor{ID: onlineID, Attributes: map[string]string{
			"name": "online-2", "oldName": "/online", labels.Schedule: "@every 10m",
		}},
	})
	sink.waitFor(t, map[string]string{"web": "@hourly", "online-2": "@every 10m"}, 1)

	// A container started after the list (created stopped, say) is recorded
	// as it starts.
	send(t, conn, containerEvent(events.ActionStart, map[string]string{"name": "db", labels.Schedule: "@daily"}))
	sink.waitFor(t, map[string]string{"web": "@hourly", "online-2": "@every 10m", "db": "@daily"}, 1)

	// Events the source doesn't follow change nothing.
	send(t, conn, containerEvent(events.ActionDie, map[string]string{"name": "db"}))
	sink.waitFor(t, map[string]string{"web": "@hourly", "online-2": "@every 10m", "db": "@daily"}, 1)

	// The removals named the containers by ID: the destroyed web, and the
	// renamed container under its old name.
	assert.Equal(t, []string{"web:id-web", "online:" + onlineID}, sink.removals())
	assert.Equal(t, onlineID, sink.id("online-2"), "the renamed container keeps its ID")

	stream.mu.Lock()
	options := stream.options[0]
	stream.mu.Unlock()

	assert.Equal(t, map[string]bool{"container": true}, options.Filters["type"])
	assert.Equal(t, map[string]bool{"create": true, "start": true, "destroy": true, "rename": true}, options.Filters["event"])

	// The subscription reaches back a little before it was made, so a change
	// around the list can't fall between the two.
	since, err := strconv.ParseFloat(options.Since, 64)
	require.NoError(t, err)
	assert.Less(t, since, float64(time.Now().Unix()), "events are asked for from before subscribing")
	assert.Greater(t, since, float64(time.Now().Add(-time.Minute).Unix()), "but only a few seconds before")
}

// TestDockerSourceAppliesTheFilter verifies that containers the filter rejects count as having no schedule.
func TestDockerSourceAppliesTheFilter(t *testing.T) {
	t.Parallel()

	inScope := func(c types.FilterableContainer) bool {
		scope, _ := c.Scope()

		return scope == "games"
	}

	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		SettleDelay: -1,
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

// TestDockerSourceReconnects verifies that a failed or ended stream is followed again, listing afresh.
func TestDockerSourceReconnects(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		containers = []types.FilterableContainer{labeled("web", map[string]string{labels.Schedule: "@every 10m"})}
	)

	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		SettleDelay: -1,
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

// TestDockerSourceRetriesAFailedList verifies that a failed list is retried on a new connection.
func TestDockerSourceRetriesAFailedList(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls int
	)

	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		SettleDelay: -1,
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

// TestDockerSourceStopsWithItsContext verifies that the source returns when its context ends.
func TestDockerSourceStopsWithItsContext(t *testing.T) {
	t.Parallel()

	stream := newFakeStream()
	source := &DockerSource{Events: stream, List: lister(), SettleDelay: -1}

	_, stop := startSource(t, source)

	stream.connected(t)
	stop()
}

// TestDockerSourceRefreshListsAgain verifies that a refresh lists again on the same connection.
func TestDockerSourceRefreshListsAgain(t *testing.T) {
	t.Parallel()

	var (
		mu         sync.Mutex
		containers = []types.FilterableContainer{labeled("web", map[string]string{labels.Schedule: "@every 10m"})}
	)

	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		SettleDelay: -1,
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

// TestDockerSourceRefreshBeforeRunIsHarmless verifies that refreshes before the source runs neither block nor panic.
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

// Write calls the function.
func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// TestDockerSourceLogsQuietlyUntilItKeepsFailing verifies the log levels of lost and restored connections.
func TestDockerSourceLogsQuietlyUntilItKeepsFailing(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		failNext = 3
	)

	logger, levels := logLevels()
	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		SettleDelay: -1,
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

// TestDockerSourcePassesIDsAndInvalidLabels verifies that container IDs and labels that don't parse reach the sink.
func TestDockerSourcePassesIDsAndInvalidLabels(t *testing.T) {
	t.Parallel()

	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		SettleDelay: -1,
		List: lister(
			labeled("web", map[string]string{labels.Schedule: " '@every 10m' "}),
			labeled("typo", map[string]string{labels.Schedule: "every ten minutes"}),
			labeled("blank", map[string]string{labels.Schedule: "  "}),
		),
	}

	sink, stop := startSource(t, source)
	defer stop()

	conn := stream.connected(t)

	// The label as written (normalized), invalid or not: the sink warns about
	// one that doesn't parse. A blank one is no schedule.
	sink.waitFor(t, map[string]string{"web": "@every 10m", "typo": "every ten minutes"}, 1)
	assert.Equal(t, "id-web", sink.id("web"), "a listed container's ID is passed on")

	send(t, conn, events.Message{
		Type: events.ContainerEventType, Action: events.ActionCreate,
		Actor: events.Actor{ID: "id-online", Attributes: map[string]string{"name": "online", labels.Schedule: "@hourly"}},
	})
	sink.waitFor(t, map[string]string{"web": "@every 10m", "typo": "every ten minutes", "online": "@hourly"}, 1)
	assert.Equal(t, "id-online", sink.id("online"), "an event's container ID is passed on")
}

// TestDockerSourceListsAgainOnceSettled verifies that the containers are
// listed once more shortly after connecting (the daemon registers the
// subscription after it starts answering), and only once.
func TestDockerSourceListsAgainOnceSettled(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		lists int
	)

	stream := newFakeStream()
	source := &DockerSource{
		Events: stream,
		List: func(context.Context) ([]types.FilterableContainer, error) {
			mu.Lock()
			defer mu.Unlock()

			lists++

			return []types.FilterableContainer{labeled("web", map[string]string{labels.Schedule: "@every 10m"})}, nil
		},
		SettleDelay: 20 * time.Millisecond,
	}

	sink, stop := startSource(t, source)
	defer stop()

	stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@every 10m"}, 2)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, 2, lists, "listed on connecting and once more, no more")
}

// listedWeb returns the container listed as web, with an ID, a creation time and labels.
func listedWeb(containerID string, created time.Time, containerLabels map[string]string) types.FilterableContainer {
	c := labeled("web", containerLabels).(interface {
		ContainerInfo() *dockerContainer.InspectResponse
	})
	c.ContainerInfo().ID = containerID
	c.ContainerInfo().Created = created.Format(time.RFC3339Nano)

	return c.(types.FilterableContainer)
}

// TestDockerSourceIgnoresReplayedNewsOfAReplacedContainer verifies that a
// replayed start of the container a listed one replaced (older than the
// listed one, by the daemon's clock) is ignored, even with another schedule,
// while news after it is applied.
func TestDockerSourceIgnoresReplayedNewsOfAReplacedContainer(t *testing.T) {
	t.Parallel()

	created := time.Now().Add(-time.Second)
	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		List:        lister(listedWeb("new", created, map[string]string{labels.Schedule: "@every 10m"})),
		SettleDelay: -1,
	}

	sink, stop := startSource(t, source)
	defer stop()

	conn := stream.connected(t)
	sink.waitFor(t, map[string]string{"web": "@every 10m"}, 1)

	event := func(id string, at time.Time, spec string) events.Message {
		return events.Message{
			Type: events.ContainerEventType, Action: events.ActionStart, TimeNano: at.UnixNano(),
			Actor: events.Actor{ID: id, Attributes: map[string]string{"name": "web", labels.Schedule: spec}},
		}
	}

	send(t, conn, event("old", created.Add(-time.Minute), "@hourly"))
	// (an event the source ignores, as a barrier: the one before it has been handled)
	send(t, conn, containerEvent(events.ActionDie, map[string]string{"name": "web"}))

	view, _ := sink.snapshot()
	assert.Equal(t, map[string]string{"web": "@every 10m"}, view, "the replaced container's replayed start was ignored")
	assert.Equal(t, "new", sink.id("web"))

	send(t, conn, event("new", created.Add(time.Millisecond), "@every 10m"))
	sink.waitFor(t, map[string]string{"web": "@every 10m"}, 1)

	// A later container under the name (created after the listed one) is news
	send(t, conn, event("newer", created.Add(time.Minute), "@hourly"))
	sink.waitFor(t, map[string]string{"web": "@hourly"}, 1)
}

// TestDockerSourceRenameKeepsTheRegistrysTimer verifies, through a real
// registry, that a container alone on its schedule keeps the schedule's entry
// when renamed.
func TestDockerSourceRenameKeepsTheRegistrysTimer(t *testing.T) {
	t.Parallel()

	timers := newFakeTimers()
	registry := NewRegistry(nil, timers)
	stream := newFakeStream()
	source := &DockerSource{
		Events: stream,
		// (the container keeps its ID through the rename, as Docker's do)
		List:        lister(listedWeb("c0ffee", time.Now().Add(-time.Hour), map[string]string{labels.Schedule: "@every 10m"})),
		SettleDelay: -1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- source.Run(ctx, registry) }()

	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()

	conn := stream.connected(t)

	require.Eventually(t, func() bool { return len(registry.Schedules()) == 1 }, 5*time.Second, 10*time.Millisecond)

	added := timers.registrations()

	send(t, conn, events.Message{
		Type: events.ContainerEventType, Action: events.ActionRename,
		Actor: events.Actor{ID: "c0ffee", Attributes: map[string]string{
			"name": "web-2", "oldName": "/web", labels.Schedule: "@every 10m",
		}},
	})
	// (a later event on the same connection: the rename has been applied by then)
	send(t, conn, containerEvent(events.ActionDie, map[string]string{"name": "web-2"}))

	assert.Equal(t, []string{"@every 10m"}, registry.Schedules())
	assert.Equal(t, added, timers.registrations(), "the schedule's entry and next run were kept")

	// The renamed container's removal, by its ID, takes the schedule with it:
	// the old name was let go
	send(t, conn, events.Message{
		Type: events.ContainerEventType, Action: events.ActionDestroy,
		Actor: events.Actor{ID: "c0ffee", Attributes: map[string]string{"name": "web-2"}},
	})
	send(t, conn, containerEvent(events.ActionDie, map[string]string{"name": "web-2"}))

	assert.Empty(t, registry.Schedules())
}

// TestDockerSourceIgnoresAReplayedRenameOntoAReplacement verifies, through a
// real registry, that a replayed rename of the container a listed one
// replaced (older than the listed one) neither moves the name to the old
// container's schedule nor restarts the replacement's.
func TestDockerSourceIgnoresAReplayedRenameOntoAReplacement(t *testing.T) {
	t.Parallel()

	created := time.Now().Add(-time.Second)
	timers := newFakeTimers()
	registry := NewRegistry(nil, timers)
	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		List:        lister(listedWeb("a1b2c3", created, map[string]string{labels.Schedule: "@every 10m"})),
		SettleDelay: -1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- source.Run(ctx, registry) }()

	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()

	conn := stream.connected(t)

	require.Eventually(t, func() bool { return len(registry.Schedules()) == 1 }, 5*time.Second, 10*time.Millisecond)

	added := timers.registrations()

	send(t, conn, events.Message{
		Type: events.ContainerEventType, Action: events.ActionRename, TimeNano: created.Add(-time.Minute).UnixNano(),
		Actor: events.Actor{ID: "d4e5f6", Attributes: map[string]string{
			"name": "web", "oldName": "/web-old", labels.Schedule: "@hourly",
		}},
	})
	send(t, conn, containerEvent(events.ActionDie, map[string]string{"name": "web"}))

	assert.Equal(t, []string{"@every 10m"}, registry.Schedules(), "the name kept the replacement's schedule")
	assert.Equal(t, added, timers.registrations(), "and its entry was never dropped and added again")
}

func TestDockerSourceIgnoredRenameLeavesTheOldName(t *testing.T) {
	t.Parallel()

	created := time.Now().Add(-time.Second)
	registry := NewRegistry(nil, newFakeTimers())
	stream := newFakeStream()
	source := &DockerSource{
		Events:      stream,
		List:        lister(listedWeb("a1b2c3", created, map[string]string{labels.Schedule: "@every 10m"})),
		SettleDelay: -1,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- source.Run(ctx, registry) }()

	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()

	conn := stream.connected(t)

	require.Eventually(t, func() bool { return len(registry.Schedules()) == 1 }, 5*time.Second, 10*time.Millisecond)

	// Replayed from before the list: a container created under another name,
	// then renamed onto web (and since replaced by the one listed there).
	before := created.Add(-time.Minute)
	attrs := map[string]string{"name": "web-old", labels.Schedule: "@hourly"}

	send(t, conn, events.Message{
		Type: events.ContainerEventType, Action: events.ActionCreate, TimeNano: before.UnixNano(),
		Actor: events.Actor{ID: "d4e5f6", Attributes: attrs},
	})
	require.Eventually(t, func() bool { return len(registry.Schedules()) == 2 }, 5*time.Second, 10*time.Millisecond)

	send(t, conn, events.Message{
		Type: events.ContainerEventType, Action: events.ActionRename, TimeNano: before.Add(time.Second).UnixNano(),
		Actor: events.Actor{ID: "d4e5f6", Attributes: map[string]string{
			"name": "web", "oldName": "/web-old", labels.Schedule: "@hourly",
		}},
	})
	send(t, conn, containerEvent(events.ActionDie, map[string]string{"name": "web"}))

	assert.Equal(t, []string{"@every 10m"}, registry.Schedules(),
		"the rename is ignored on web, but the old name's schedule went with it")
}
