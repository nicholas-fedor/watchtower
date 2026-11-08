package percontainer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/rs/zerolog"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Source reports which containers carry which schedule to a [Sink], keeping it
// current until ctx ends. It is the only part that knows the container runtime.
type Source interface {
	Run(ctx context.Context, sink Sink) error
}

// Refresher is a [Source] that can be asked to read the containers again,
// in case it missed a change. The scheduler asks after each update run.
type Refresher interface {
	Refresh()
}

// EventStream is the part of Docker's client a [DockerSource] uses: its event
// stream. The Docker client satisfies it.
type EventStream interface {
	Events(ctx context.Context, options dockerClient.EventsListOptions) dockerClient.EventsResult
}

// Lister lists the containers as they are now.
type Lister func(ctx context.Context) ([]types.FilterableContainer, error)

// Retry delays between attempts to reach Docker's event stream.
const (
	defaultRetryDelay    = time.Second
	defaultMaxRetryDelay = time.Minute
	// Each failed attempt multiplies the delay by this.
	retryBackoff = 2
	// A connection that lasted this long counts as healthy: the delay and the
	// count of failures start again (one that drops at once keeps backing off).
	healthyConnection = time.Minute
	// Failed attempts in a row before a lost stream is logged as a warning:
	// one dropped connection that reconnects at once is routine (a daemon
	// restart, a proxy's idle timeout) and logged at debug level.
	warnAfterFailures = 3
	// Events are asked for from this long before subscribing, so the stream
	// covers the moments around the list however long the subscription takes
	// to reach the daemon (and a little difference in clocks): what's replayed
	// is applied in order after the list, and comes out the same.
	replayWindow = 5 * time.Second
	// The containers are listed once more this long after connecting: the
	// daemon starts sending before it has registered the subscription, and a
	// change in that moment, older than the replay window by a clock that's
	// behind ours, would otherwise be missed until the next run.
	defaultSettleDelay = 2 * time.Second
)

var errStreamClosed = errors.New("docker event stream closed")

// DockerSource follows Docker's container events. On connecting it subscribes,
// from a few seconds back (so no change can fall between the subscription and
// the list), lists the containers once, then applies each create, start,
// destroy and rename as it comes. Containers are told apart by ID as well as
// name, so the news of a container that has since been replaced under the same
// name (as an update replaces one) never touches its replacement.
//
// It recovers on its own from what can go wrong with a long-lived stream:
//
//   - When the stream ends or fails (the daemon restarted, a proxy cut an idle
//     connection) it reconnects with a growing delay and lists again, since
//     events missed meanwhile are not replayed.
//   - [DockerSource.Refresh] lists again while connected: the scheduler calls
//     it after each update run, so a connection that died without an error (a
//     remote daemon behind a dropped TCP connection) is caught at the next
//     run, without polling.
//
// Containers the filter rejects (another scope, not enabled, excluded by
// name) count as having no schedule, as they would in a run.
type DockerSource struct {
	// Events is Docker's event stream.
	Events EventStream
	// List lists the containers when connecting and on a refresh.
	List Lister
	// Filter is the containers Watchtower manages. Nil manages all.
	Filter types.Filter
	// Log for connection trouble and changes. Nil logs nothing.
	Log     *zerolog.Logger
	refresh chan struct{}

	// listed is the last list: each container's ID and creation time by name,
	// to tell a replayed event about a container since replaced (see apply).
	// Used only by the goroutine following the stream.
	listed map[string]listedContainer
	// RetryDelay is the first wait before reconnecting; it doubles up to
	// MaxRetryDelay. Zero uses one second and one minute.
	RetryDelay, MaxRetryDelay time.Duration
	// SettleDelay is how long after connecting the containers are listed
	// once more (see defaultSettleDelay). Zero uses two seconds; negative
	// lists only on connecting.
	SettleDelay time.Duration

	refreshOnce sync.Once
}

// listedContainer is a listed container's ID and when Docker created it.
type listedContainer struct {
	created time.Time
	id      string
}

var (
	_ Source    = (*DockerSource)(nil)
	_ Refresher = (*DockerSource)(nil)
)

// Refresh implements [Refresher]: the containers are listed again when the
// source is next free to. Requests while one is pending are merged; a request
// while disconnected is met by the list made on reconnecting.
func (s *DockerSource) Refresh() {
	select {
	case s.refreshes() <- struct{}{}:
	default:
	}
}

// Run implements [Source]. It returns nil once ctx ends.
func (s *DockerSource) Run(ctx context.Context, sink Sink) error {
	log := s.logger()

	delay, most := s.RetryDelay, s.MaxRetryDelay
	if delay <= 0 {
		delay = defaultRetryDelay
	}

	if most < delay {
		most = max(defaultMaxRetryDelay, delay)
	}

	wait := delay
	failures := 0

	for {
		began := time.Now()
		connected, err := s.follow(ctx, sink, func() {
			if failures >= warnAfterFailures {
				log.Info().Msg("Reconnected to Docker's container events: container schedules are current")
			}
		})

		// Stopping ends the stream too: that's no failure to report.
		if ctx.Err() != nil {
			return nil //nolint:nilerr // shutdown, not an error
		}

		if connected && time.Since(began) >= healthyConnection {
			failures = 0
			wait = delay
		}

		failures++

		level := zerolog.DebugLevel
		if failures >= warnAfterFailures {
			level = zerolog.WarnLevel
		}

		log.WithLevel(level).Err(err).Bool("was_connected", connected).Int("attempt", failures).Dur("retry_in", wait).
			Msg("Lost Docker's container events: reconnecting (container schedules may be out of date until then)")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}

		wait = min(wait*retryBackoff, most)
	}
}

// refreshes is the channel refresh requests wait on (one at most), made on
// first use so a zero DockerSource works.
func (s *DockerSource) refreshes() chan struct{} {
	s.refreshOnce.Do(func() { s.refresh = make(chan struct{}, 1) })

	return s.refresh
}

// logger is the source's logger, or one that logs nothing.
func (s *DockerSource) logger() *zerolog.Logger {
	if s.Log != nil {
		return s.Log
	}

	nop := zerolog.Nop()

	return &nop
}

// follow subscribes, lists, then applies events (and refreshes) until the
// stream ends. It calls connected once the first list is applied, and reports
// whether it got that far.
func (s *DockerSource) follow(ctx context.Context, sink Sink, connected func()) (bool, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream := s.Events.Events(streamCtx, dockerClient.EventsListOptions{
		Since: eventTime(time.Now().Add(-replayWindow)),
		Filters: make(dockerClient.Filters).
			Add("type", string(events.ContainerEventType)).
			Add("event", string(events.ActionCreate), string(events.ActionStart),
				string(events.ActionDestroy), string(events.ActionRename)),
	})

	// A refresh asked for while disconnected is met by this list.
	select {
	case <-s.refreshes():
	default:
	}

	err := s.resync(ctx, sink, "connected")
	if err != nil {
		return false, err
	}

	s.logger().Debug().Msg("Following Docker's container events for container schedules")

	connected()

	var settled <-chan time.Time

	switch {
	case s.SettleDelay == 0:
		settled = time.After(defaultSettleDelay)
	case s.SettleDelay > 0:
		settled = time.After(s.SettleDelay)
	}

	for {
		select {
		case <-ctx.Done():
			return true, nil
		case <-settled:
			// (once: the subscription is registered by now)
			settled = nil

			err := s.resync(ctx, sink, "settled")
			if err != nil {
				return true, err
			}
		case err, open := <-stream.Err:
			if !open || err == nil {
				return true, errStreamClosed
			}

			return true, fmt.Errorf("docker event stream: %w", err)
		case msg, open := <-stream.Messages:
			if !open {
				return true, errStreamClosed
			}

			s.apply(sink, msg)
		case <-s.refreshes():
			// (events that arrive meanwhile wait in the stream, in order, so
			// none is lost and none is applied ahead of the list)
			err := s.resync(ctx, sink, "after a run")
			if err != nil {
				return true, err
			}
		}
	}
}

// resync lists the containers and replaces what the sink knows with them.
// Why (connecting, after a run) is logged. Events that arrived meanwhile wait
// in the stream and are applied after it, in order; any that describe a
// container already replaced are told apart by its ID.
func (s *DockerSource) resync(ctx context.Context, sink Sink, why string) error {
	containers, err := s.List(ctx)
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	snapshot := make(map[string]Labeled, len(containers))
	s.listed = make(map[string]listedContainer, len(containers))

	for _, c := range containers {
		s.listed[c.Name()] = listedContainer{id: idOf(c), created: createdOf(c)}

		if spec, ok := s.scheduleOf(c); ok {
			snapshot[c.Name()] = Labeled{ID: idOf(c), Spec: spec}
		}
	}

	s.logger().Debug().
		Str("reason", why).
		Int("containers", len(containers)).
		Int("with_own_schedule", len(snapshot)).
		Msg("Read the containers for container schedules")

	sink.Reset(snapshot)

	return nil
}

// apply applies one container event to the sink: a destroy removes the
// container (that one, by ID), a rename moves it, and a create or start
// records it with the schedule its labels carry.
func (s *DockerSource) apply(sink Sink, msg events.Message) {
	if msg.Type != events.ContainerEventType {
		return
	}

	attrs := msg.Actor.Attributes
	name := container.NormalizeContainerName(attrs["name"])

	if name == "" {
		return
	}

	switch msg.Action { //nolint:exhaustive // only the actions subscribed to
	case events.ActionDestroy:
		s.logger().Debug().Str("container", name).Str("id", shortID(msg.Actor.ID)).Msg("Container event: removed")
		sink.Remove(name, msg.Actor.ID)
	case events.ActionRename:
		// (under the new name first: a container alone on its schedule keeps
		// the schedule's entry and next run through the rename)
		s.setFrom(sink, name, msg.Actor.ID, attrs, msg.Action)

		if old := container.NormalizeContainerName(attrs["oldName"]); old != "" && old != name {
			sink.Remove(old, msg.Actor.ID)
		}
	case events.ActionCreate, events.ActionStart:
		if s.replaced(name, msg) {
			s.logger().Debug().Str("container", name).Str("id", shortID(msg.Actor.ID)).Str("event", string(msg.Action)).
				Msg("Ignoring a replayed event of a container replaced since (older than the one listed under its name)")

			return
		}

		s.setFrom(sink, name, msg.Actor.ID, attrs, msg.Action)
	default:
	}
}

// setFrom records a container from an event: its attributes carry its labels,
// with its name and image.
func (s *DockerSource) setFrom(sink Sink, name, containerID string, attrs map[string]string, action events.Action) {
	spec, _ := s.scheduleOf(eventContainer(name, containerID, attrs))
	s.logEvent(name, attrs, spec, action)
	sink.Set(name, containerID, spec)
}

// logEvent logs a container event at debug level: the container, its
// schedule (or the global one), and the name it was renamed from.
func (s *DockerSource) logEvent(name string, attrs map[string]string, spec string, action events.Action) {
	event := s.logger().Debug().Str("container", name).Str("event", string(action))
	if spec == "" {
		event = event.Str("schedule", "global")
	} else {
		event = event.Str("schedule", spec)
	}

	if old := attrs["oldName"]; old != "" {
		event = event.Str("renamed_from", container.NormalizeContainerName(old))
	}

	event.Msg("Container event")
}

// scheduleOf returns the container's schedule label, normalized, when the
// filter manages the container and the label is set. The value isn't checked
// here: the [Sink] warns about one that doesn't parse, as documented.
func (s *DockerSource) scheduleOf(c types.FilterableContainer) (string, bool) {
	if s.Filter != nil && !s.Filter(c) {
		return "", false
	}

	raw, ok := c.GetLabel(labels.Schedule)
	if !ok {
		return "", false
	}

	spec := Normalize(raw)

	return spec, spec != ""
}

// replaced reports whether an event is about another container than the one
// listed under its name, from before that one was created: news replayed from
// before the list, of a container it had already replaced. Both times are the
// daemon's, so the clocks' difference doesn't matter.
func (s *DockerSource) replaced(name string, msg events.Message) bool {
	listed, ok := s.listed[name]
	if !ok || listed.id == "" || listed.id == msg.Actor.ID || listed.created.IsZero() || msg.TimeNano == 0 {
		return false
	}

	return time.Unix(0, msg.TimeNano).Before(listed.created)
}

// createdOf returns when Docker created a container, when it says (a listed
// container does).
func createdOf(c types.FilterableContainer) time.Time {
	withInfo, ok := c.(interface {
		ContainerInfo() *dockerContainer.InspectResponse
	})
	if !ok || withInfo.ContainerInfo() == nil {
		return time.Time{}
	}

	created, err := time.Parse(time.RFC3339Nano, withInfo.ContainerInfo().Created)
	if err != nil {
		return time.Time{}
	}

	return created
}

// idOf returns a container's ID when it has one (a listed container does).
func idOf(c types.FilterableContainer) string {
	if withID, ok := c.(interface{ ID() types.ContainerID }); ok {
		return string(withID.ID())
	}

	return ""
}

// eventTime is a time as Docker's events API takes it: Unix seconds, with
// the nanoseconds after a point.
func eventTime(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10) + "." + fmt.Sprintf("%09d", t.Nanosecond())
}

// eventContainer is a container as an event describes it, read the way
// Watchtower reads any container (labels, scope, enable, name, image), so a
// filter treats it as it would in a run.
func eventContainer(name, containerID string, attrs map[string]string) types.FilterableContainer {
	containerLabels := make(map[string]string, len(attrs))

	for k, v := range attrs {
		if k == "name" || k == "image" || k == "oldName" {
			continue
		}

		containerLabels[k] = v
	}

	info := &dockerContainer.InspectResponse{
		ID:     containerID,
		Name:   "/" + name,
		Config: &dockerContainer.Config{Image: attrs["image"], Labels: containerLabels},
	}

	return container.NewContainer(nil, info, nil)
}
