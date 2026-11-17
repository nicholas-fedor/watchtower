package schedule

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// scheduledContainer is a container for container schedule tests: a name and
// an optional schedule label.
type scheduledContainer struct {
	name     string
	schedule string
	labeled  bool
}

func (c scheduledContainer) Name() string            { return c.name }
func (c scheduledContainer) IsWatchtower() bool      { return false }
func (c scheduledContainer) Enabled() (bool, bool)   { return false, false }
func (c scheduledContainer) Scope() (string, bool)   { return "", false }
func (c scheduledContainer) NormalizedScope() string { return "none" }
func (c scheduledContainer) ImageName() string       { return c.name + ":latest" }

func (c scheduledContainer) GetLabel(key string) (string, bool) {
	if key == labels.Schedule && c.labeled {
		return c.schedule, true
	}

	return "", false
}

// unlabeled returns a container on the global schedule.
func unlabeled(name string) scheduledContainer { return scheduledContainer{name: name} }

// labeled returns a container with a schedule label.
func labeled(name, spec string) scheduledContainer {
	return scheduledContainer{name: name, schedule: spec, labeled: true}
}

// containerList is the containers a test's Docker holds, which it can change.
type containerList struct {
	containers []filters.FilterableContainer
	err        error
	calls      int
	mu         sync.Mutex
}

func (l *containerList) set(containers ...scheduledContainer) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.containers = l.containers[:0]
	for _, c := range containers {
		l.containers = append(l.containers, c)
	}
}

// list is the scheduler's own listing, counted.
func (l *containerList) list(ctx context.Context) ([]filters.FilterableContainer, error) {
	l.mu.Lock()
	l.calls++
	l.mu.Unlock()

	return l.session(ctx)
}

// session is the listing an update session makes.
func (l *containerList) session(context.Context) ([]filters.FilterableContainer, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]filters.FilterableContainer(nil), l.containers...), l.err
}

func (l *containerList) listCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.calls
}

// scheduledRun is one update session the scheduler started: which of the
// test's containers it checked, and whether it named containers at all.
type scheduledRun struct {
	due    []string
	at     time.Time
	hasDue bool
}

// runRecorder records the scheduler's update sessions.
type runRecorder struct {
	list *containerList
	runs []scheduledRun
	mu   sync.Mutex
}

func (r *runRecorder) runUpdate(ctx context.Context, _ filters.Filter, params updateParams.Params) *metrics.Metric {
	containers, err := r.list.session(ctx)
	if err == nil && params.Listed != nil {
		params.Listed(containers)
	}

	run := scheduledRun{at: time.Now(), hasDue: params.Due != nil}

	for _, c := range containers {
		if params.Due == nil || params.Due(c) {
			run.due = append(run.due, c.Name())
		}
	}

	r.mu.Lock()
	r.runs = append(r.runs, run)
	r.mu.Unlock()

	return &metrics.Metric{}
}

func (r *runRecorder) snapshot() []scheduledRun {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]scheduledRun(nil), r.runs...)
}

// runContainerSchedules runs the scheduler with container schedules on, for
// the given time, and returns its sessions.
func runContainerSchedules(
	t *testing.T,
	list *containerList,
	globalSpec string,
	filter filters.Filter,
	during func(elapsed time.Duration),
	steps ...time.Duration,
) []scheduledRun {
	t.Helper()

	recorder := &runRecorder{list: list}

	deps := testDeps(t, recorder.runUpdate, func(context.Context, logging.StartupParams) {})
	deps.ScheduleSpec = globalSpec
	deps.ContainerSchedules = list.list

	if filter != nil {
		deps.Filter = filter
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

	var elapsed time.Duration

	for _, step := range steps {
		time.Sleep(step)
		synctest.Wait()

		elapsed += step
		if during != nil {
			during(elapsed)
		}
	}

	cancel()
	require.NoError(t, <-done)

	return recorder.snapshot()
}

// TestContainerSchedules_FiringTogetherShareOneSession verifies that the
// global schedule and a container schedule firing at the same moment run one
// session, which checks the containers of both.
func TestContainerSchedules_FiringTogetherShareOneSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(unlabeled("global"), labeled("even", "*/2 * * * * *"))

		runs := runContainerSchedules(t, list, "@every 1s", nil, nil, 2500*time.Millisecond)

		require.Len(t, runs, 2)
		assert.True(t, runs[0].hasDue)
		assert.Equal(t, []string{"global"}, runs[0].due)
		assert.ElementsMatch(t, []string{"global", "even"}, runs[1].due)
	})
}

// TestContainerSchedules_ContainerScheduleChecksOnlyItsContainers verifies
// that a container schedule's session checks only the containers on it, and
// the global schedule's only the containers without one.
func TestContainerSchedules_ContainerScheduleChecksOnlyItsContainers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(unlabeled("a"), unlabeled("b"), labeled("c", "@every 3s"))

		runs := runContainerSchedules(t, list, "@every 5s", nil, nil, 5500*time.Millisecond)

		require.Len(t, runs, 2)
		assert.Equal(t, []string{"c"}, runs[0].due)
		assert.ElementsMatch(t, []string{"a", "b"}, runs[1].due)
	})
}

// TestContainerSchedules_SessionCoversEveryManagedContainer verifies that a
// container schedule's session runs over the containers Watchtower manages,
// not only the due ones, so the session sees their dependencies.
func TestContainerSchedules_SessionCoversEveryManagedContainer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(unlabeled("a"), labeled("c", "@every 1s"))

		var (
			mu          sync.Mutex
			sawFilter   filters.Filter
			paramFilter filters.Filter
		)

		managed := func(filters.FilterableContainer) bool { return true }

		deps := testDeps(t, func(_ context.Context, filter filters.Filter, params updateParams.Params) *metrics.Metric {
			mu.Lock()
			sawFilter, paramFilter = filter, params.Filter
			mu.Unlock()

			return &metrics.Metric{}
		}, func(context.Context, logging.StartupParams) {})
		deps.Filter = managed
		deps.ContainerSchedules = list.list

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		time.Sleep(1500 * time.Millisecond)
		synctest.Wait()
		cancel()
		require.NoError(t, <-done)

		mu.Lock()
		defer mu.Unlock()

		require.NotNil(t, sawFilter)
		assert.True(t, sawFilter(unlabeled("a")), "the session left out a container that is not due")
		assert.True(t, paramFilter(unlabeled("a")))
	})
}

// TestContainerSchedules_LabelsReadAfterEachSession verifies that a schedule
// label added, changed, or removed takes effect after the next session, read
// from the session's own listing: the scheduler lists the containers only at
// startup.
func TestContainerSchedules_LabelsReadAfterEachSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(unlabeled("a"))

		runs := runContainerSchedules(t, list, "@every 10s", nil, func(elapsed time.Duration) {
			switch elapsed {
			case 5 * time.Second:
				// New container with its own schedule, before the global run.
				list.set(unlabeled("a"), labeled("b", "@every 1s"))
			case 13 * time.Second:
				// Its label is removed: it is back on the global schedule.
				list.set(unlabeled("a"), unlabeled("b"))
			}
		}, 5*time.Second, 8*time.Second, 7500*time.Millisecond)

		var gotB, globalWithB int

		for _, run := range runs {
			for _, name := range run.due {
				if name == "b" && len(run.due) == 1 {
					gotB++
				}

				if name == "b" && len(run.due) == 2 {
					globalWithB++
				}
			}
		}

		// Runs at 10s (global, then b's schedule is read), 11s and 12s and 13s
		// (b alone; the 13s run reads the removed label), 20s (global with b).
		assert.Equal(t, 3, gotB, "runs: %+v", runs)
		assert.Equal(t, 1, globalWithB, "runs: %+v", runs)
		assert.Equal(t, 1, list.listCalls(), "the scheduler listed the containers after startup")
	})
}

// TestContainerSchedules_InvalidLabelFollowsGlobalSchedule verifies that a
// container whose schedule label does not parse follows the global schedule.
func TestContainerSchedules_InvalidLabelFollowsGlobalSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(unlabeled("a"), labeled("b", "not a schedule"), labeled("c", `"@every 2s"`))

		runs := runContainerSchedules(t, list, "@every 3s", nil, nil, 3500*time.Millisecond)

		require.Len(t, runs, 2)
		assert.Equal(t, []string{"c"}, runs[0].due, "a quoted label is read without its quotes")
		assert.ElementsMatch(t, []string{"a", "b"}, runs[1].due)
	})
}

// TestContainerSchedules_WithoutGlobalSchedule verifies that container
// schedules run when there is no global schedule, and that the containers
// without one are then not updated.
func TestContainerSchedules_WithoutGlobalSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(unlabeled("a"), labeled("b", "@every 2s"))

		runs := runContainerSchedules(t, list, "", nil, nil, 4500*time.Millisecond)

		require.Len(t, runs, 2)

		for _, run := range runs {
			assert.Equal(t, []string{"b"}, run.due)
		}
	})
}

// TestContainerSchedules_UnmanagedContainersBringNoSchedule verifies that a
// container Watchtower does not manage adds no schedule.
func TestContainerSchedules_UnmanagedContainersBringNoSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(labeled("managed", "@every 3s"), labeled("other", "@every 2s"))

		onlyManaged := func(c filters.FilterableContainer) bool { return c.Name() == "managed" }

		runs := runContainerSchedules(t, list, "", onlyManaged, nil, 3500*time.Millisecond)

		require.Len(t, runs, 1)
		assert.Contains(t, runs[0].due, "managed")
	})
}

// TestContainerSchedules_ListFailureKeepsSchedules verifies that a failed
// listing keeps the schedules in use.
func TestContainerSchedules_ListFailureKeepsSchedules(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(labeled("b", "@every 1s"))

		runs := runContainerSchedules(t, list, "", nil, func(elapsed time.Duration) {
			if elapsed == 1500*time.Millisecond {
				list.mu.Lock()
				list.err = errors.New("daemon unavailable")
				list.mu.Unlock()
			}
		}, 1500*time.Millisecond, 2*time.Second)

		assert.Len(t, runs, 3)
	})
}

// TestContainerSchedules_UpdateOnStartChecksGlobalContainers verifies that the
// update at startup checks the containers on the global schedule.
func TestContainerSchedules_UpdateOnStartChecksGlobalContainers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(unlabeled("a"), labeled("b", "@every 1h"))

		recorder := &runRecorder{list: list}
		deps := testDeps(t, recorder.runUpdate, func(context.Context, logging.StartupParams) {})
		deps.ContainerSchedules = list.list
		deps.UpdateOnStart = true

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		synctest.Wait()
		cancel()
		require.NoError(t, <-done)

		runs := recorder.snapshot()
		require.Len(t, runs, 1)
		assert.Equal(t, []string{"a"}, runs[0].due)
	})
}

// TestContainerSchedules_Off verifies that without container schedules a
// session checks every container and the schedule label is ignored.
func TestContainerSchedules_Off(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		list := &containerList{}
		list.set(unlabeled("a"), labeled("b", "@every 1s"))

		recorder := &runRecorder{list: list}
		deps := testDeps(t, recorder.runUpdate, func(context.Context, logging.StartupParams) {})
		deps.ScheduleSpec = "@every 2s"

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		time.Sleep(2500 * time.Millisecond)
		synctest.Wait()
		cancel()
		require.NoError(t, <-done)

		runs := recorder.snapshot()
		require.Len(t, runs, 1)
		assert.False(t, runs[0].hasDue)
		assert.ElementsMatch(t, []string{"a", "b"}, runs[0].due)
	})
}
