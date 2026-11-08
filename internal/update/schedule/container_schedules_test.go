package schedule

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/schedule/percontainer"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// sourceFunc is a percontainer.Source from a function.
type sourceFunc func(ctx context.Context, sink percontainer.Sink) error

// Run calls the function.
func (f sourceFunc) Run(ctx context.Context, sink percontainer.Sink) error { return f(ctx, sink) }

// testContainer returns a container with this name and these labels.
func testContainer(name string, containerLabels map[string]string) types.FilterableContainer {
	return container.NewContainer(nil, &dockerContainer.InspectResponse{
		Name:   "/" + name,
		Config: &dockerContainer.Config{Image: "example/" + name + ":latest", Labels: containerLabels},
	}, nil)
}

// TestRunUpgradesOnSchedule_ContainerSchedules verifies that a container with
// its own schedule label runs on that schedule, and that the global schedule
// leaves it out.
func TestRunUpgradesOnSchedule_ContainerSchedules(t *testing.T) {
	// (the metrics singleton starts a goroutine: outside the bubble)
	metrics.Default()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		var (
			mu   sync.Mutex
			runs []types.Filter
		)

		runUpdate := func(_ context.Context, filter types.Filter, _ updateParams.Params) *metrics.Metric {
			mu.Lock()
			defer mu.Unlock()

			runs = append(runs, filter)

			return &metrics.Metric{}
		}

		deps := testDeps(t, runUpdate, func(context.Context, logging.StartupParams) {})
		deps.ScheduleSpec = "@every 25m"
		deps.UpdateOnStart = true
		deps.ContainerSchedules = sourceFunc(func(ctx context.Context, sink percontainer.Sink) error {
			sink.Reset(map[string]percontainer.Labeled{"games": {ID: "games-id", Spec: "@every 10m"}})
			<-ctx.Done()

			return nil
		})

		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		// Startup, the container schedule at 10 and 20 minutes, then the
		// global schedule's first periodic run at 25 (apart from the others).
		time.Sleep(26 * time.Minute)
		synctest.Wait()

		cancel()
		require.NoError(t, <-done)

		games := testContainer("games", map[string]string{labels.Schedule: "@every 10m"})
		plain := testContainer("plain", nil)

		mu.Lock()
		defer mu.Unlock()

		require.Len(t, runs, 4)

		assert.False(t, runs[0](games), "the global run leaves out a container with its own schedule")
		assert.True(t, runs[0](plain), "the global run covers the rest")

		assert.True(t, runs[1](games), "the container's own schedule runs it")
		assert.False(t, runs[1](plain), "a container schedule runs only its containers")
		assert.True(t, runs[2](games), "and again on its next tick")
		assert.False(t, runs[3](games), "a periodic global run leaves it out too")
		assert.True(t, runs[3](plain), "and covers the rest")
	})
}

// TestRunUpgradesOnSchedule_ContainerSchedulesSkipFirstRun verifies that after
// a self-update cleanup, each schedule's first run skips Watchtower's own
// update, container schedules included (Watchtower may follow one).
func TestRunUpgradesOnSchedule_ContainerSchedulesSkipFirstRun(t *testing.T) {
	// (the metrics singleton starts a goroutine: outside the bubble)
	metrics.Default()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		var (
			mu    sync.Mutex
			skips []bool
		)

		runUpdate := func(_ context.Context, _ types.Filter, params updateParams.Params) *metrics.Metric {
			mu.Lock()
			defer mu.Unlock()

			skips = append(skips, params.SkipSelfUpdate)

			return &metrics.Metric{}
		}

		deps := testDeps(t, runUpdate, func(context.Context, logging.StartupParams) {})
		deps.ScheduleSpec = "@every 25m"
		deps.ReplacedOlderInstance = true
		deps.ContainerSchedules = sourceFunc(func(ctx context.Context, sink percontainer.Sink) error {
			sink.Set("watchtower", "wt-id", "@every 10m")
			<-ctx.Done()

			return nil
		})

		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		// The container schedule at 10 and 20 minutes, the global one at 25.
		time.Sleep(26 * time.Minute)
		synctest.Wait()

		cancel()
		require.NoError(t, <-done)

		mu.Lock()
		defer mu.Unlock()

		assert.Equal(t, []bool{true, false, true}, skips,
			"each schedule's first run skips the self-update, later ones don't")
	})
}

// TestRunUpgradesOnSchedule_ContainerSchedulesWithoutGlobalSchedule verifies
// that container schedules run when there is no global schedule.
func TestRunUpgradesOnSchedule_ContainerSchedulesWithoutGlobalSchedule(t *testing.T) {
	// (the metrics singleton starts a goroutine: outside the bubble)
	metrics.Default()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		var calls atomic.Int32

		runUpdate := func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			calls.Add(1)

			return &metrics.Metric{}
		}

		deps := testDeps(t, runUpdate, func(context.Context, logging.StartupParams) {})
		deps.ContainerSchedules = sourceFunc(func(ctx context.Context, sink percontainer.Sink) error {
			sink.Set("games", "games-id", "@every 10m")
			<-ctx.Done()

			return nil
		})

		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		time.Sleep(21 * time.Minute)
		synctest.Wait()

		cancel()
		require.NoError(t, <-done)

		assert.Equal(t, int32(2), calls.Load())
	})
}

// refreshingSource is a source counting the refreshes it is asked for.
type refreshingSource struct {
	sourceFunc

	refreshes atomic.Int32
}

// Refresh counts a refresh.
func (s *refreshingSource) Refresh() { s.refreshes.Add(1) }

// TestRunUpgradesOnSchedule_ContainerSchedulesRefreshAfterRuns verifies that
// the source is asked to read the containers again after each run.
func TestRunUpgradesOnSchedule_ContainerSchedulesRefreshAfterRuns(t *testing.T) {
	metrics.Default()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		runUpdate := func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			return &metrics.Metric{}
		}

		source := &refreshingSource{sourceFunc: func(ctx context.Context, sink percontainer.Sink) error {
			sink.Set("games", "games-id", "@every 10m")
			<-ctx.Done()

			return nil
		}}

		deps := testDeps(t, runUpdate, func(context.Context, logging.StartupParams) {})
		deps.ScheduleSpec = "@every 1h"
		deps.UpdateOnStart = true
		deps.ContainerSchedules = source

		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		// The run at startup, then the container schedule twice.
		time.Sleep(21 * time.Minute)
		synctest.Wait()

		cancel()
		require.NoError(t, <-done)

		assert.Equal(t, int32(3), source.refreshes.Load())
	})
}

// TestSuperviseSource verifies that a source that panics or stops early is
// started again, with a growing delay, until the context ends.
func TestSuperviseSource(t *testing.T) {
	metrics.Default()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		var (
			starts atomic.Int32
			times  []time.Duration
		)

		began := time.Now()
		source := sourceFunc(func(ctx context.Context, _ percontainer.Sink) error {
			times = append(times, time.Since(began))

			switch starts.Add(1) {
			case 1:
				panic("listener bug")
			case 2:
				return errors.New("gave up")
			case 3:
				return nil // (returned before being stopped)
			default:
				<-ctx.Done()

				return nil
			}
		})

		done := make(chan struct{})

		go func() {
			defer close(done)

			superviseSource(ctx, newTestLogger(t), source, percontainer.NewRegistry(nil, nil), time.Second, time.Minute)
		}()

		time.Sleep(time.Hour)
		synctest.Wait()

		cancel()
		<-done

		assert.Equal(t, int32(4), starts.Load(), "started again after a panic, an error and an early return")
		assert.Equal(t, []time.Duration{0, time.Second, 3 * time.Second, 7 * time.Second}, times, "the delay doubles")
	})
}
