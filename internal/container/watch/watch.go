// Package watch follows Docker's container events, so Watchtower can react to
// containers starting between its scheduled runs instead of finding out at
// the next one.
package watch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"

	dockerClient "github.com/moby/moby/client"
)

// EventStream is the part of the container client that streams Docker's
// events.
type EventStream interface {
	Events(ctx context.Context, options dockerClient.EventsListOptions) dockerClient.EventsResult
}

// Change is a container that started.
type Change struct {
	// Received is when Watchtower received the event.
	Received time.Time
	// Action is what happened: start.
	Action events.Action
	// Name is the container's name, without the leading slash.
	Name string
	// ID is the container's ID.
	ID string
}

// Retry delays for a lost event stream.
const (
	defaultRetryDelay    = time.Second
	defaultMaxRetryDelay = time.Minute
	retryBackoff         = 2
	// warnAfterFailures is the number of failed connections in a row that
	// are logged at debug before one is logged as a warning.
	warnAfterFailures = 3
)

// errStreamClosed is an event stream that ended without an error.
var errStreamClosed = errors.New("docker event stream closed")

// Watcher follows Docker's container events.
type Watcher struct {
	Events EventStream
	Log    *zerolog.Logger
	// RetryDelay and MaxRetryDelay bound the wait before reconnecting to a
	// lost event stream, which doubles from one to the other. Zero uses one
	// second and one minute.
	RetryDelay, MaxRetryDelay time.Duration
}

// Run follows the container events until ctx ends, calling changed for each.
// It calls changed from one goroutine, in the order the events arrive.
//
// A lost stream is reconnected after a growing delay. The new stream starts
// where the last one ended, so no event in between is missed while the
// daemon still holds it.
//
// Parameters:
//   - ctx: Ends the watch.
//   - changed: Called for each container change.
func (w *Watcher) Run(ctx context.Context, changed func(Change)) {
	delay := w.retryDelay()
	failures := 0

	var since time.Time

	for ctx.Err() == nil {
		received, err := w.follow(ctx, since, changed)
		if ctx.Err() != nil {
			return
		}

		if !received.IsZero() {
			since = received
			delay = w.retryDelay()
			failures = 0
		}

		failures++

		level := zerolog.DebugLevel
		if failures >= warnAfterFailures {
			level = zerolog.WarnLevel
		}

		w.Log.WithLevel(level).Err(err).Dur("retry_in", delay).Msg("Lost Docker's container events: reconnecting")

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		delay = min(delay*retryBackoff, w.maxRetryDelay())
	}
}

// follow streams the events from since (or from now when zero) until the
// stream ends. It returns the daemon time of the last event received, or
// zero when none was, and why the stream ended.
func (w *Watcher) follow(ctx context.Context, since time.Time, changed func(Change)) (time.Time, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	options := dockerClient.EventsListOptions{
		Filters: make(dockerClient.Filters).
			Add("type", string(events.ContainerEventType)).
			Add("event", string(events.ActionStart)),
	}

	if !since.IsZero() {
		// The event at since was already handled. Docker's since is inclusive.
		options.Since = eventTime(since.Add(time.Nanosecond))
	}

	stream := w.Events.Events(streamCtx, options)

	w.Log.Debug().Msg("Following Docker's container events")

	var last time.Time

	for {
		select {
		case <-ctx.Done():
			return last, nil
		case err, open := <-stream.Err:
			if !open || err == nil {
				return last, errStreamClosed
			}

			return last, fmt.Errorf("docker event stream: %w", err)
		case msg, open := <-stream.Messages:
			if !open {
				return last, errStreamClosed
			}

			last = time.Unix(0, msg.TimeNano)

			change := Change{
				Received: time.Now(),
				Action:   msg.Action,
				Name:     strings.TrimPrefix(msg.Actor.Attributes["name"], "/"),
				ID:       msg.Actor.ID,
			}

			w.Log.Debug().
				Str("container", change.Name).
				Str("event", string(change.Action)).
				Msg("Container event")

			changed(change)
		}
	}
}

func (w *Watcher) retryDelay() time.Duration {
	if w.RetryDelay > 0 {
		return w.RetryDelay
	}

	return defaultRetryDelay
}

func (w *Watcher) maxRetryDelay() time.Duration {
	if w.MaxRetryDelay > 0 {
		return w.MaxRetryDelay
	}

	return defaultMaxRetryDelay
}

// eventTime formats a time as Docker's events API takes it: seconds and
// nanoseconds since the epoch.
func eventTime(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10) + "." + fmt.Sprintf("%09d", t.Nanosecond())
}

// FromClient returns the container changes to follow when enabled
// (--watch-events), through the client's event stream. It returns nil when
// the option is off, or when the client cannot stream events, which is
// logged.
//
// Parameters:
//   - enabled: Whether to follow container events.
//   - client: Container client, used when it streams events.
//   - log: Process logger.
//
// Returns:
//   - func(context.Context, func(Change)): Follows the changes until the
//     context ends, or nil.
func FromClient(enabled bool, client any, log *zerolog.Logger) func(context.Context, func(Change)) {
	if !enabled {
		return nil
	}

	stream, ok := client.(EventStream)
	if !ok {
		log.Warn().Msg("Watching events needs Docker's event stream, which this client lacks")

		return nil
	}

	watcher := &Watcher{Events: stream, Log: log}

	return watcher.Run
}
