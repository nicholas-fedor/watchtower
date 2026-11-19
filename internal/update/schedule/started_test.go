package schedule

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/moby/moby/api/types/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/watch"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// namedContainer is a container for started-check tests. Its ID is its
// name with "-id", unless id is set.
type namedContainer struct {
	name       string
	id         string
	watchtower bool
}

func (c namedContainer) ID() container.ContainerID {
	if c.id != "" {
		return container.ContainerID(c.id)
	}

	return container.ContainerID(c.name + "-id")
}

func (c namedContainer) Name() string                   { return c.name }
func (c namedContainer) IsWatchtower() bool             { return c.watchtower }
func (c namedContainer) Enabled() (bool, bool)          { return false, false }
func (c namedContainer) Scope() (string, bool)          { return "", false }
func (c namedContainer) NormalizedScope() string        { return "none" }
func (c namedContainer) ImageName() string              { return c.name + ":latest" }
func (c namedContainer) GetLabel(string) (string, bool) { return "", false }

// fakeChanges is a container event stream the test feeds.
type fakeChanges struct {
	changed func(watch.Change)
	ready   chan struct{}
}

func newFakeChanges() *fakeChanges { return &fakeChanges{ready: make(chan struct{})} }

func (f *fakeChanges) follow(ctx context.Context, changed func(watch.Change)) {
	f.changed = changed
	close(f.ready)
	<-ctx.Done()
}

func (f *fakeChanges) send(action events.Action, name string) {
	f.sendID(action, name, name+"-id")
}

func (f *fakeChanges) sendID(action events.Action, name, id string) {
	<-f.ready
	f.changed(watch.Change{Received: time.Now(), Action: action, Name: name, ID: id})
}

// startedRun is one session a started check ran.
type startedRun struct {
	at          time.Duration
	due         []string
	managedSeen bool
}

// startedHarness runs the scheduler with container events and no schedule.
type startedHarness struct {
	changes    *fakeChanges
	start      time.Time
	containers []namedContainer
	runs       []startedRun
	hold       chan struct{}
	cancel     context.CancelFunc
	done       chan error
	mu         sync.Mutex
}

func startStartedHarness(t *testing.T, containers ...namedContainer) *startedHarness {
	t.Helper()

	h := &startedHarness{changes: newFakeChanges(), start: time.Now(), containers: containers, done: make(chan error, 1)}

	deps := testDeps(t, func(_ context.Context, filter filters.Filter, params updateParams.Params) *metrics.Metric {
		run := startedRun{at: time.Since(h.start), managedSeen: filter != nil}

		for _, c := range h.containers {
			if params.Due != nil && params.Due(c) {
				run.due = append(run.due, c.name)
			}
		}

		h.mu.Lock()
		h.runs = append(h.runs, run)
		hold := h.hold
		h.mu.Unlock()

		if hold != nil {
			<-hold
		}

		return &metrics.Metric{}
	}, func(context.Context, logging.StartupParams) {})
	deps.ContainerChanges = h.changes.follow

	ctx, cancel := context.WithCancel(t.Context())
	h.cancel = cancel

	go func() { h.done <- RunUpgradesOnSchedule(ctx, deps) }()

	synctest.Wait()

	h.start = time.Now()

	return h
}

func (h *startedHarness) wait(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func (h *startedHarness) stop(t *testing.T) []startedRun {
	t.Helper()

	h.cancel()
	require.NoError(t, <-h.done)

	h.mu.Lock()
	defer h.mu.Unlock()

	return h.runs
}

// TestStartedChecks_NewContainerIsChecked verifies that a started container
// is checked, alone, once no other new container has started for the quiet
// period, in a session over every managed container.
func TestStartedChecks_NewContainerIsChecked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := startStartedHarness(t, namedContainer{name: "web"}, namedContainer{name: "db"})

		h.changes.send(events.ActionStart, "web")
		h.wait(9 * time.Second)

		h.mu.Lock()
		assert.Empty(t, h.runs, "checked before the quiet period")
		h.mu.Unlock()

		h.wait(2 * time.Second)

		runs := h.stop(t)
		require.Len(t, runs, 1)
		assert.Equal(t, []string{"web"}, runs[0].due)
		assert.True(t, runs[0].managedSeen)
	})
}

// TestStartedChecks_ComposeRecreate verifies the sequence Compose uses to
// recreate a container: the replacement is created under a temporary name,
// renamed, then started. It is checked under the name it has by then, matched
// by its ID.
func TestStartedChecks_ComposeRecreate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := startStartedHarness(t, namedContainer{name: "comfyui", id: "new-id"})

		h.changes.sendID(events.ActionCreate, "6965376ae359_comfyui", "new-id")
		h.changes.sendID(events.ActionDestroy, "comfyui", "old-id")
		h.changes.sendID(events.ActionRename, "comfyui", "new-id")
		h.changes.sendID(events.ActionStart, "comfyui", "new-id")
		h.wait(11 * time.Second)

		runs := h.stop(t)
		require.Len(t, runs, 1)
		assert.Equal(t, []string{"comfyui"}, runs[0].due)
	})
}

// TestStartedChecks_MatchedByIDNotName verifies that a container renamed after
// it started is still checked.
func TestStartedChecks_MatchedByIDNotName(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := startStartedHarness(t, namedContainer{name: "web", id: "abc"})

		h.changes.sendID(events.ActionStart, "temporary_web", "abc")
		h.wait(11 * time.Second)

		runs := h.stop(t)
		require.Len(t, runs, 1)
		assert.Equal(t, []string{"web"}, runs[0].due)
	})
}

// TestStartedChecks_ProjectCheckedTogether verifies that containers started
// close together are checked in one session.
func TestStartedChecks_ProjectCheckedTogether(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := startStartedHarness(t, namedContainer{name: "a"}, namedContainer{name: "b"}, namedContainer{name: "c"})

		h.changes.send(events.ActionStart, "a")
		h.wait(3 * time.Second)
		h.changes.send(events.ActionStart, "b")
		h.wait(3 * time.Second)
		h.changes.send(events.ActionStart, "c")
		h.wait(11 * time.Second)

		runs := h.stop(t)
		require.Len(t, runs, 1)
		assert.Equal(t, 16*time.Second, runs[0].at.Round(time.Second))
		assert.ElementsMatch(t, []string{"a", "b", "c"}, runs[0].due)
	})
}

// TestStartedChecks_LongestWait verifies that containers started without a
// pause are checked a minute after the first.
func TestStartedChecks_LongestWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		names := make([]namedContainer, 0, 20)
		for i := range 20 {
			names = append(names, namedContainer{name: string(rune('a' + i))})
		}

		h := startStartedHarness(t, names...)

		for i := range 14 {
			h.changes.send(events.ActionStart, names[i].name)
			h.wait(5 * time.Second)
		}

		runs := h.stop(t)
		require.NotEmpty(t, runs)
		assert.Equal(t, time.Minute, runs[0].at.Round(time.Second))
		assert.Len(t, runs[0].due, 12)
	})
}

// TestStartedChecks_RestartNotCheckedAgain verifies that a container is
// checked on its first start only, and that a recreated one, with a new ID,
// is checked.
func TestStartedChecks_RestartNotCheckedAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := startStartedHarness(t, namedContainer{name: "web", id: "second"})

		h.changes.sendID(events.ActionStart, "web", "first")
		h.wait(11 * time.Second)
		h.changes.sendID(events.ActionStart, "web", "first") // restarted
		h.wait(11 * time.Second)
		h.changes.sendID(events.ActionStart, "web", "second") // recreated
		h.wait(11 * time.Second)

		runs := h.stop(t)
		require.Len(t, runs, 2, "a restart was checked again")
		assert.Empty(t, runs[0].due, "the first container is gone by the check")
		assert.Equal(t, []string{"web"}, runs[1].due)
	})
}

// TestStartedChecks_StartedDuringUpdateIsChecked verifies that a container
// started while an update session runs is checked after it.
func TestStartedChecks_StartedDuringUpdateIsChecked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := startStartedHarness(t, namedContainer{name: "first"}, namedContainer{name: "during"})

		hold := make(chan struct{})

		h.mu.Lock()
		h.hold = hold
		h.mu.Unlock()

		h.changes.send(events.ActionStart, "first")
		h.wait(11 * time.Second) // first's session starts and holds the lock

		h.changes.send(events.ActionStart, "during")
		h.wait(11 * time.Second) // during's check waits for the lock

		h.mu.Lock()
		h.hold = nil
		h.mu.Unlock()
		close(hold)
		synctest.Wait()

		runs := h.stop(t)
		require.Len(t, runs, 2)
		assert.Equal(t, []string{"first"}, runs[0].due)
		assert.Equal(t, []string{"during"}, runs[1].due)
	})
}

// TestStartedChecks_IgnoresOtherChanges verifies that other events, the old
// container of a self-update, and Watchtower itself are not checked.
func TestStartedChecks_IgnoresOtherChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := startStartedHarness(t, namedContainer{name: "gone"}, namedContainer{name: "made"},
			namedContainer{name: "moved"}, namedContainer{name: "watchtower-old-abc"},
			namedContainer{name: "watchtower", watchtower: true}, namedContainer{name: "web"})

		h.changes.send(events.ActionDestroy, "gone")
		h.changes.send(events.ActionCreate, "made")
		h.changes.send(events.ActionRename, "moved")
		h.changes.send(events.ActionStart, "watchtower-old-abc")
		h.changes.send(events.ActionStart, "watchtower")
		h.wait(11 * time.Second)

		h.changes.send(events.ActionStart, "web")
		h.wait(11 * time.Second)

		runs := h.stop(t)
		require.Len(t, runs, 2)
		assert.Empty(t, runs[0].due, "Watchtower itself is never due")
		assert.Equal(t, []string{"web"}, runs[1].due)
	})
}

// TestStartedChecks_Off verifies that without container events no check runs.
func TestStartedChecks_Off(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deps := testDeps(t, func(context.Context, filters.Filter, updateParams.Params) *metrics.Metric {
			t.Error("an update ran")

			return &metrics.Metric{}
		}, func(context.Context, logging.StartupParams) {})

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		time.Sleep(time.Minute)
		synctest.Wait()
		cancel()
		require.NoError(t, <-done)
	})
}

// TestStartedChecks_NotStartedAfterShutdown verifies that no watch starts
// once the context has ended, as in API-only mode, where the scheduler is
// reached only at shutdown.
func TestStartedChecks_NotStartedAfterShutdown(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	followed := false
	wait := watchStarted(ctx, newTestLogger(t), func(context.Context, func(watch.Change)) { followed = true },
		func(filters.Filter) {})
	wait()

	assert.False(t, followed)
}
