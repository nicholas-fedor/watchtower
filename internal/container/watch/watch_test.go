package watch

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerClient "github.com/moby/moby/client"
)

// connection is one event stream the fake hands out.
type connection struct {
	options  dockerClient.EventsListOptions
	messages chan events.Message
	errs     chan error
}

// fakeStream hands out one connection per Events call.
type fakeStream struct {
	connections []*connection
	mu          sync.Mutex
}

func (f *fakeStream) Events(_ context.Context, options dockerClient.EventsListOptions) dockerClient.EventsResult {
	c := &connection{options: options, messages: make(chan events.Message, 8), errs: make(chan error, 1)}

	f.mu.Lock()
	f.connections = append(f.connections, c)
	f.mu.Unlock()

	return dockerClient.EventsResult{Messages: c.messages, Err: c.errs}
}

func (f *fakeStream) connection(i int) *connection {
	f.mu.Lock()
	defer f.mu.Unlock()

	if i >= len(f.connections) {
		return nil
	}

	return f.connections[i]
}

func (f *fakeStream) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.connections)
}

// message returns a container event for a container named name.
func message(action events.Action, name string, at time.Time) events.Message {
	return events.Message{
		Type:     events.ContainerEventType,
		Action:   action,
		Actor:    events.Actor{ID: name + "-id", Attributes: map[string]string{"name": name}},
		TimeNano: at.UnixNano(),
	}
}

// recorder records the changes a watcher reports.
type recorder struct {
	changes []Change
	mu      sync.Mutex
}

func (r *recorder) changed(change Change) {
	r.mu.Lock()
	r.changes = append(r.changes, change)
	r.mu.Unlock()
}

func (r *recorder) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	names := make([]string, 0, len(r.changes))
	for _, c := range r.changes {
		names = append(names, string(c.Action)+" "+c.Name)
	}

	return names
}

func nopLogger() *zerolog.Logger {
	log := zerolog.Nop()

	return &log
}

// TestWatcher_ReportsContainerChanges verifies that the watcher subscribes to
// container start events, and reports each with the container's name without
// its slash.
func TestWatcher_ReportsContainerChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := &fakeStream{}
		got := &recorder{}
		watcher := &Watcher{Events: stream, Log: nopLogger()}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			watcher.Run(ctx, got.changed)
		}()

		synctest.Wait()

		conn := stream.connection(0)
		require.NotNil(t, conn)
		assert.Equal(t, []string{"container"}, filterValues(conn.options.Filters, "type"))
		assert.ElementsMatch(t, []string{"start"}, filterValues(conn.options.Filters, "event"))
		assert.Empty(t, conn.options.Since, "a first connection starts from now")

		conn.messages <- message(events.ActionCreate, "/web", time.Now())

		conn.messages <- message(events.ActionDestroy, "db", time.Now())

		synctest.Wait()

		assert.Equal(t, []string{"create web", "destroy db"}, got.names())

		cancel()
		<-done
	})
}

// TestWatcher_ReconnectsFromLastEvent verifies that a lost stream is
// reconnected after a delay, from just after the last event received.
func TestWatcher_ReconnectsFromLastEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := &fakeStream{}
		got := &recorder{}
		watcher := &Watcher{Events: stream, Log: nopLogger()}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			watcher.Run(ctx, got.changed)
		}()

		synctest.Wait()

		last := time.Unix(1_700_000_000, 5)

		first := stream.connection(0)
		first.messages <- message(events.ActionCreate, "web", last)

		first.errs <- errors.New("daemon restarted")

		synctest.Wait()

		assert.Equal(t, 1, stream.count(), "reconnected without waiting")

		time.Sleep(time.Second)
		synctest.Wait()

		require.Equal(t, 2, stream.count())
		assert.Equal(t, "1700000000.000000006", stream.connection(1).options.Since)

		cancel()
		<-done
	})
}

// TestWatcher_RetryDelayGrows verifies that the delay before reconnecting
// doubles while the stream keeps failing, up to its bound.
func TestWatcher_RetryDelayGrows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := &fakeStream{}
		watcher := &Watcher{Events: stream, Log: nopLogger(), RetryDelay: time.Second, MaxRetryDelay: 4 * time.Second}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			watcher.Run(ctx, func(Change) {})
		}()

		gaps := make([]time.Duration, 0, 4)

		synctest.Wait()

		for i := range 4 {
			close(stream.connection(i).messages)

			lost := time.Now()

			for stream.count() == i+1 {
				time.Sleep(100 * time.Millisecond)
				synctest.Wait()
			}

			gaps = append(gaps, time.Since(lost).Round(time.Second))
		}

		assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}, gaps)

		cancel()
		<-done
	})
}

// TestFromClient verifies that the watch is off unless enabled and the client
// streams events.
func TestFromClient(t *testing.T) {
	t.Parallel()

	assert.Nil(t, FromClient(false, &fakeStream{}, nopLogger()))
	assert.Nil(t, FromClient(true, struct{}{}, nopLogger()))
	assert.NotNil(t, FromClient(true, &fakeStream{}, nopLogger()))
}

// filterValues returns the values of one key of an events filter.
func filterValues(f dockerClient.Filters, key string) []string {
	values := make([]string, 0, len(f[key]))
	for value := range f[key] {
		values = append(values, value)
	}

	return values
}
