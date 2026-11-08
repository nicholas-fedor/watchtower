package schedule

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestMain starts the metrics goroutine, which the first [metrics.Default] call
// starts lazily, before any test runs. When that first call comes from a
// scheduled or startup run inside a synctest bubble, the goroutine belongs to
// the bubble and leaves it deadlocked when its test ends. Starting it here
// keeps it outside every bubble, whatever order the tests run in.
func TestMain(m *testing.M) {
	metrics.Default()

	os.Exit(m.Run())
}

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// testDeps returns ScheduleDeps with common test defaults.
// Callers override fields such as ScheduleSpec, UpdateOnStart, ReplacedOlderInstance, and BaseParams as needed.
func testDeps(
	t *testing.T,
	runUpdate func(context.Context, types.Filter, updateParams.Params) *metrics.Metric,
	writeStartup func(context.Context, logging.StartupParams),
) ScheduleDeps {
	t.Helper()

	return ScheduleDeps{
		Logger:              newTestLogger(t),
		Filter:              filters.NoFilter,
		WriteStartupMessage: writeStartup,
		RunUpdate:           runUpdate,
		BaseParams:          updateParams.Params{},
	}
}

// skippedScansTotal returns the process-wide watchtower_scans_skipped_total
// counter, which the scheduler records through the default metrics handler.
// It reports failures with t.Errorf, so it is safe to call from a condition
// that require.Eventually runs on another goroutine.
//
// Parameters:
//   - t: Test that receives any gather failure.
//
// Returns:
//   - float64: Current counter value, or 0 after a failure.
func skippedScansTotal(t *testing.T) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Errorf("gather metrics: %v", err)

		return 0
	}

	for _, family := range families {
		if family.GetName() == "watchtower_scans_skipped_total" {
			return family.GetMetric()[0].GetCounter().GetValue()
		}
	}

	t.Errorf("watchtower_scans_skipped_total is not registered")

	return 0
}

// TestWaitForRunningUpdate verifies that the shutdown wait takes the update
// lock and keeps it, waiting for a running update to finish first, and that
// the wait ends when its timeout passes or its context is canceled.
func TestWaitForRunningUpdate(t *testing.T) {
	t.Parallel()

	t.Run("no update running", func(t *testing.T) {
		t.Parallel()

		lock := NewUpdateLock()

		waitForRunningUpdate(newTestLogger(t), t.Context(), lock)

		assert.False(t, lock.TryLock(), "an update could start after the shutdown wait")
	})

	t.Run("update running", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			lock := NewUpdateLock()
			require.True(t, lock.TryLock())

			done := make(chan struct{})

			go func() {
				waitForRunningUpdate(newTestLogger(t), t.Context(), lock)
				close(done)
			}()

			synctest.Wait()

			select {
			case <-done:
				t.Fatal("the shutdown wait returned while an update was running")
			default:
			}

			lock.Unlock()
			<-done

			assert.False(t, lock.TryLock(), "an update could start after the shutdown wait")
		})
	})

	t.Run("update outlasts the wait", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			lock := NewUpdateLock()
			require.True(t, lock.TryLock())

			start := time.Now()

			waitForRunningUpdate(newTestLogger(t), t.Context(), lock)

			assert.Equal(t, 60*time.Second, time.Since(start))
		})
	})

	t.Run("context canceled", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			lock := NewUpdateLock()
			require.True(t, lock.TryLock())

			ctx, cancel := context.WithCancel(t.Context())
			start := time.Now()

			go func() {
				time.Sleep(50 * time.Millisecond)
				cancel()
			}()

			waitForRunningUpdate(newTestLogger(t), ctx, lock)

			assert.Equal(t, 50*time.Millisecond, time.Since(start))
		})
	})
}

// TestSkipIfStillRunning verifies that a scheduled run that starts while the
// previous one is still running is skipped and counted as a skipped scan, and
// that a run after the previous one finishes runs normally. It reads the
// process-wide skipped scans counter, so it does not run in parallel.
func TestSkipIfStillRunning(t *testing.T) {
	before := skippedScansTotal(t)

	started := make(chan struct{})
	release := make(chan struct{})

	var runs atomic.Int32

	job := skipIfStillRunning(newTestLogger(t))(cron.FuncJob(func() {
		if runs.Add(1) == 1 {
			close(started)
			<-release
		}
	}))

	done := make(chan struct{})

	go func() {
		job.Run()
		close(done)
	}()

	<-started
	job.Run()

	assert.Equal(t, int32(1), runs.Load(), "the overlapping run is skipped")
	assert.InDelta(t, before+1, skippedScansTotal(t), 0)

	close(release)
	<-done
	job.Run()

	assert.Equal(t, int32(2), runs.Load(), "a run after the previous one finishes runs")
	assert.InDelta(t, before+1, skippedScansTotal(t), 0)
}

// TestRecoverPanics verifies that a panic in a scheduled run is recovered and
// logged through the scheduler's logger in one error line with notify=no. The
// line carries the panic value and the stack. A run after the panic runs
// normally.
func TestRecoverPanics(t *testing.T) {
	t.Parallel()

	log, output := logcapture.New(zerolog.DebugLevel)

	var runs atomic.Int32

	job := recoverPanics(log)(cron.FuncJob(func() {
		if runs.Add(1) == 1 {
			panic("boom")
		}
	}))

	require.NotPanics(t, job.Run)
	job.Run()

	assert.Equal(t, int32(2), runs.Load(), "a run after the panic runs")

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 1)

	assert.Contains(t, lines[0], "level=error")
	assert.Contains(t, lines[0], `msg="Scheduled run panicked"`)
	assert.Contains(t, lines[0], "error=boom")
	assert.Contains(t, lines[0], "notify=no")
	assert.Contains(t, lines[0], "TestRecoverPanics")
}

// TestRunUpgradesOnSchedule_RecoversPanickingRun verifies that the scheduler's
// cron entry logs a panicking scheduled run through the scheduler's logger and
// keeps running, so the next scheduled update still runs.
func TestRunUpgradesOnSchedule_RecoversPanickingRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		log, output := logcapture.New(zerolog.DebugLevel)

		var runs atomic.Int32

		deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			if runs.Add(1) == 1 {
				panic("boom")
			}

			return &metrics.Metric{}
		}, func(context.Context, logging.StartupParams) {})
		deps.Logger = log
		deps.ScheduleSpec = "@every 1s"

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		// The run at 1 second panics, and the run at 2 seconds follows it.
		time.Sleep(2500 * time.Millisecond)
		synctest.Wait()

		cancel()
		require.NoError(t, <-done)

		assert.Equal(t, int32(2), runs.Load(), "the run after the panic did not start")
		assert.Contains(t, output.String(), `msg="Scheduled run panicked" error=boom`)
	})
}

// TestRunUpgradesOnSchedule_OverlappingRunIsSkipped verifies that the
// scheduler's cron entry skips a run that comes due while the previous
// scheduled update is still running, so only one update runs, and that the
// skip is counted as a skipped scan.
//
// The test reads the process-wide skipped scans counter, so it does not run in
// parallel.
func TestRunUpgradesOnSchedule_OverlappingRunIsSkipped(t *testing.T) {
	before := skippedScansTotal(t)

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})

		var runs atomic.Int32

		deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			runs.Add(1)
			<-release

			return &metrics.Metric{}
		}, func(context.Context, logging.StartupParams) {})
		deps.ScheduleSpec = "@every 1s"

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		// The run at 1 second holds the update, and the run at 2 seconds overlaps it.
		time.Sleep(2500 * time.Millisecond)
		synctest.Wait()

		assert.Equal(t, int32(1), runs.Load(), "the overlapping run started an update")
		assert.InDelta(t, before+1, skippedScansTotal(t), 0)

		cancel()
		close(release)

		require.NoError(t, <-done)
		assert.Equal(t, int32(1), runs.Load(), "an update ran after shutdown")
	})
}

// TestRunUpgradesOnSchedule_WaitingRunStopsAtShutdown verifies that a
// scheduled run waiting for the update lock gives up when the scheduler shuts
// down, instead of starting an update once the lock comes free.
func TestRunUpgradesOnSchedule_WaitingRunStopsAtShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var updated atomic.Bool

		deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			updated.Store(true)

			return &metrics.Metric{}
		}, func(context.Context, logging.StartupParams) {})

		deps.Lock = NewUpdateLock()
		deps.ScheduleSpec = "@every 1s"

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		// Another update takes the lock after the startup message and holds it
		// while the scheduled run comes due.
		synctest.Wait()
		require.True(t, deps.Lock.TryLock())

		time.Sleep(1500 * time.Millisecond)
		cancel()

		require.NoError(t, <-done)

		deps.Lock.Unlock()
		synctest.Wait()

		assert.False(t, updated.Load(), "a scheduled run started an update after shutdown")
	})
}

// TestRunUpgradesOnSchedule_ShutdownWaitsForRunningUpdate verifies that at
// shutdown the scheduler waits for a running update and returns once the
// update releases the update lock. The update may come from another caller,
// such as the HTTP API, or from the scheduler's own schedule.
func TestRunUpgradesOnSchedule_ShutdownWaitsForRunningUpdate(t *testing.T) {
	t.Parallel()

	t.Run("update from another caller", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			deps := testDeps(t, nil, func(context.Context, logging.StartupParams) {})
			deps.Lock = NewUpdateLock()

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)

			go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

			// Another update takes the lock after the startup message, which
			// holds the lock while it is written.
			synctest.Wait()
			require.True(t, deps.Lock.TryLock())

			// Cancel, then let the scheduler reach its wait for the update.
			cancel()
			synctest.Wait()

			select {
			case err := <-done:
				t.Fatalf("scheduler stopped while an update was still running: %v", err)
			default:
			}

			deps.Lock.Unlock()
			require.NoError(t, <-done)
		})
	})

	t.Run("scheduled update", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			release := make(chan struct{})

			var running atomic.Bool

			deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
				running.Store(true)
				<-release

				return &metrics.Metric{}
			}, func(context.Context, logging.StartupParams) {})
			deps.Lock = NewUpdateLock()
			deps.ScheduleSpec = "@every 1s"

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)

			go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

			// The run at 1 second starts and holds the update lock.
			time.Sleep(1500 * time.Millisecond)
			synctest.Wait()
			require.True(t, running.Load(), "the scheduled update did not start")

			// Cancel, then let the scheduler reach its wait for the update.
			cancel()
			synctest.Wait()

			select {
			case err := <-done:
				t.Fatalf("scheduler stopped while its scheduled update was still running: %v", err)
			default:
			}

			close(release)
			require.NoError(t, <-done)
		})
	})
}

func TestRunUpgradesOnSchedule_EmptySchedule(t *testing.T) {
	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ updateParams.Params) *metrics.Metric {
		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(context.Context, logging.StartupParams) {}

	// Use timeout to avoid hanging
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)
	err := RunUpgradesOnSchedule(timeoutCtx, deps)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// TestRunUpgradesOnSchedule_UpdateOnStart verifies that update on start runs
// one update before the scheduler waits for cancellation.
func TestRunUpgradesOnSchedule_UpdateOnStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var updates atomic.Int32

		deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			updates.Add(1)

			return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
		}, func(context.Context, logging.StartupParams) {})
		deps.UpdateOnStart = true

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		synctest.Wait()
		assert.Equal(t, int32(1), updates.Load(), "update on start ran a different number of updates")

		cancel()
		require.NoError(t, <-done)
	})
}

// TestRunUpgradesOnSchedule_UpdateOnStartWithSchedule verifies that with a
// schedule, update on start runs one update at startup and the schedule's
// first run follows a full interval later. The update at startup neither
// repeats nor replaces the scheduled run.
func TestRunUpgradesOnSchedule_UpdateOnStartWithSchedule(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var updates atomic.Int32

		deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			updates.Add(1)

			return &metrics.Metric{}
		}, func(context.Context, logging.StartupParams) {})
		deps.UpdateOnStart = true
		deps.ScheduleSpec = "@every 1h"

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		synctest.Wait()
		assert.Equal(t, int32(1), updates.Load(), "the update at startup did not run once")

		time.Sleep(59 * time.Minute)
		synctest.Wait()
		assert.Equal(t, int32(1), updates.Load(), "an update ran before the schedule's first run")

		time.Sleep(2 * time.Minute)
		synctest.Wait()
		assert.Equal(t, int32(2), updates.Load(), "the schedule's first run did not follow")

		cancel()
		require.NoError(t, <-done)
	})
}

// TestRunUpgradesOnSchedule_UpdateOnStartSkippedWhenCanceled verifies that the
// update at startup does not run when the process context is already canceled,
// for example by a stop signal during startup.
func TestRunUpgradesOnSchedule_UpdateOnStartSkippedWhenCanceled(t *testing.T) {
	updateCalled := false
	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ updateParams.Params) *metrics.Metric {
		updateCalled = true

		return &metrics.Metric{}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	deps := testDeps(t, runUpdatesWithNotifications, func(context.Context, logging.StartupParams) {})
	deps.UpdateOnStart = true

	err := RunUpgradesOnSchedule(ctx, deps)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if updateCalled {
		t.Error("expected the update at startup to be skipped")
	}
}

// TestRunUpgradesOnSchedule_StartupWaitsForRunningUpdate verifies that the
// startup message waits for an update that already holds the lock, such as
// one the HTTP API started as soon as it began serving. The two would
// otherwise share one notification batch. The update at startup is then
// skipped and counted as a skipped scan, as it is when it finds the lock
// taken. The startup message therefore does not announce it. The test does
// not run in parallel, because it reads the process-wide skipped scans counter.
func TestRunUpgradesOnSchedule_StartupWaitsForRunningUpdate(t *testing.T) {
	before := skippedScansTotal(t)

	synctest.Test(t, func(t *testing.T) {
		lock := NewUpdateLock()
		require.True(t, lock.TryLock())

		var updated atomic.Bool

		var announced atomic.Pointer[logging.StartupParams]

		deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			updated.Store(true)

			return &metrics.Metric{}
		}, func(_ context.Context, startup logging.StartupParams) {
			announced.Store(&startup)
		})
		deps.Lock = lock
		deps.UpdateOnStart = true

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		synctest.Wait()
		assert.Nil(t, announced.Load(), "the startup message waits for the running update")

		lock.Unlock()
		synctest.Wait()

		startup := announced.Load()
		require.NotNil(t, startup, "the startup message follows the running update")
		require.NotNil(t, startup.UpdateOnStart)
		assert.False(t, *startup.UpdateOnStart, "the startup message announces the skipped update at startup")
		assert.InDelta(t, before+1, skippedScansTotal(t), 0)

		cancel()
		require.NoError(t, <-done)
		assert.False(t, updated.Load(), "the update at startup is skipped")
	})
}

// TestRunUpgradesOnSchedule_UpdateOnStartSkippedWhileLocked verifies that the
// update at startup does not wait for an update that holds the lock. The HTTP
// API can start one right after the startup message. The skipped update is
// counted as a skipped scan. No startup message is written here, so the lock
// is left to that update. The test does not run in parallel,
// because it reads the process-wide skipped scans counter.
func TestRunUpgradesOnSchedule_UpdateOnStartSkippedWhileLocked(t *testing.T) {
	before := skippedScansTotal(t)

	synctest.Test(t, func(t *testing.T) {
		lock := NewUpdateLock()
		require.True(t, lock.TryLock())

		var updated atomic.Bool

		deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			updated.Store(true)

			return &metrics.Metric{}
		}, nil)
		deps.Lock = lock
		deps.UpdateOnStart = true

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		synctest.Wait()
		assert.InDelta(t, before+1, skippedScansTotal(t), 0)

		lock.Unlock()
		cancel()
		require.NoError(t, <-done)
		assert.False(t, updated.Load(), "the update at startup is skipped")
	})
}

// TestRunUpgradesOnSchedule_UpdateOnStartHoldsLock verifies that the update at
// startup holds the update lock until it returns. Another caller of the lock,
// such as the HTTP API, waits for the update to finish before it can start one.
func TestRunUpgradesOnSchedule_UpdateOnStartHoldsLock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lock := NewUpdateLock()
		release := make(chan struct{})

		var running atomic.Bool

		deps := testDeps(t, func(context.Context, types.Filter, updateParams.Params) *metrics.Metric {
			running.Store(true)
			<-release
			running.Store(false)

			return &metrics.Metric{}
		}, nil)
		deps.Lock = lock
		deps.UpdateOnStart = true

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		synctest.Wait()
		require.True(t, running.Load(), "the update at startup is running")

		// overlapped records whether the second caller took the lock while the
		// update at startup was still running.
		var overlapped atomic.Bool

		acquired := make(chan struct{})

		go func() {
			if lock.Lock(t.Context()) != nil {
				return
			}

			overlapped.Store(running.Load())
			close(acquired)
			lock.Unlock()
		}()

		synctest.Wait()

		select {
		case <-acquired:
			t.Fatal("another caller took the lock while the update at startup was running")
		default:
		}

		close(release)
		<-acquired
		assert.False(t, overlapped.Load(), "the lock was taken before the update at startup returned")

		cancel()
		require.NoError(t, <-done)
	})
}

// TestRunUpgradesOnSchedule_StartupSkippedAtShutdown verifies that a startup
// message that waits for a running update is skipped when the process stops
// first. A stopping process has nothing to announce.
func TestRunUpgradesOnSchedule_StartupSkippedAtShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		lock := NewUpdateLock()
		require.True(t, lock.TryLock())

		var announced atomic.Bool

		deps := testDeps(t, nil, func(context.Context, logging.StartupParams) {
			announced.Store(true)
		})
		deps.Lock = lock

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		// The scheduler sees the cancellation before the lock comes free.
		synctest.Wait()
		cancel()
		synctest.Wait()
		lock.Unlock()

		require.NoError(t, <-done)
		assert.False(t, announced.Load(), "a stopping process wrote the startup message")
	})
}

// TestRunUpgradesOnSchedule_StartupMessageGetsSchedulerContext verifies that
// the scheduler hands its own context to the startup message writer, which
// writes nothing for a stopping process. In API-only mode the scheduler starts
// only at shutdown, after the HTTP API has announced startup, so this keeps it
// from writing a second message.
func TestRunUpgradesOnSchedule_StartupMessageGetsSchedulerContext(t *testing.T) {
	t.Parallel()

	var (
		called bool
		// receivedErr is the error of the context the writer received.
		receivedErr error
	)

	deps := testDeps(t, nil, func(ctx context.Context, _ logging.StartupParams) {
		called = true
		receivedErr = ctx.Err()
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.NoError(t, RunUpgradesOnSchedule(ctx, deps))
	require.True(t, called, "the startup message writer was not called")
	assert.ErrorIs(t, receivedErr, context.Canceled, "the writer did not get the scheduler's stopped context")
}

func TestRunUpgradesOnSchedule_InvalidCronSpec(t *testing.T) {
	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ updateParams.Params) *metrics.Metric {
		return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(context.Context, logging.StartupParams) {}

	deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)
	deps.ScheduleSpec = "invalid cron spec"

	err := RunUpgradesOnSchedule(ctx, deps)
	require.ErrorContains(t, err, `invalid schedule "invalid cron spec": `)
}

func TestRunUpgradesOnSchedule_QuotedScheduleSpec(t *testing.T) {
	tests := []struct {
		name         string
		scheduleSpec string
		expectError  bool
	}{
		{
			name:         "double-quoted cron spec from Docker Compose",
			scheduleSpec: `"0 0 2 * * *"`,
			expectError:  false,
		},
		{
			name:         "single-quoted cron spec",
			scheduleSpec: `'0 0 2 * * *'`,
			expectError:  false,
		},
		{
			name:         "double-quoted descriptor",
			scheduleSpec: `"@hourly"`,
			expectError:  false,
		},
		{
			name:         "unquoted cron spec",
			scheduleSpec: `0 0 2 * * *`,
			expectError:  false,
		},
		{
			name:         "unquoted descriptor",
			scheduleSpec: `@hourly`,
			expectError:  false,
		},
		{
			name:         "double-quoted invalid spec still errors",
			scheduleSpec: `"invalid cron spec"`,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ updateParams.Params) *metrics.Metric {
				return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(context.Context, logging.StartupParams) {}

			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)
			deps.ScheduleSpec = tt.scheduleSpec
			deps.ReplacedOlderInstance = true
			err := RunUpgradesOnSchedule(timeoutCtx, deps)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestRunUpgradesOnSchedule_ContextCancellation(t *testing.T) {
	ctx := t.Context()

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ updateParams.Params) *metrics.Metric {
		return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(context.Context, logging.StartupParams) {}

	// Cancel immediately
	canceledCtx, cancelFunc := context.WithCancel(ctx)
	cancelFunc()

	deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)

	err := RunUpgradesOnSchedule(canceledCtx, deps)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// TestRunUpgradesOnSchedule_StopsOnlyOnCancellation verifies that the scheduler
// leaves signal handling to its caller. An interrupt sent to the process does
// not stop it, and canceling its context does.
//
// The interrupt goes to the whole test process, so this test must not run in
// parallel with tests that register or depend on signal handlers. It does not
// call t.Parallel, so no other test runs while it does.
func TestRunUpgradesOnSchedule_StopsOnlyOnCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sends an interrupt signal to the test process")
	}

	// Hold the interrupt so it does not end the test process.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	t.Cleanup(func() { signal.Stop(interrupts) })

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ updateParams.Params) *metrics.Metric {
		return &metrics.Metric{}
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)

	go func() {
		done <- RunUpgradesOnSchedule(ctx, testDeps(t, runUpdatesWithNotifications, func(context.Context, logging.StartupParams) {}))
	}()

	process, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, process.Signal(os.Interrupt))

	select {
	case <-interrupts:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the interrupt was not delivered")
	}

	select {
	case err := <-done:
		require.FailNow(t, "the scheduler stopped on the interrupt", "error: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the scheduler did not stop after cancellation")
	}
}

func TestRunUpgradesOnSchedule_MonitorOnlyParameter(t *testing.T) {
	tests := []struct {
		name              string
		monitorOnly       bool
		expectMonitorOnly bool
	}{
		{
			name:              "monitorOnly false",
			monitorOnly:       false,
			expectMonitorOnly: false,
		},
		{
			name:              "monitorOnly true",
			monitorOnly:       true,
			expectMonitorOnly: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			var capturedParams updateParams.Params

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params updateParams.Params) *metrics.Metric {
				capturedParams = params

				return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(context.Context, logging.StartupParams) {}

			// Use timeout to avoid hanging
			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)
			deps.UpdateOnStart = true
			deps.BaseParams.MonitorOnly = tt.monitorOnly

			err := RunUpgradesOnSchedule(timeoutCtx, deps)
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}

			if capturedParams.MonitorOnly != tt.expectMonitorOnly {
				t.Errorf(
					"expected MonitorOnly=%v, got %v",
					tt.expectMonitorOnly,
					capturedParams.MonitorOnly,
				)
			}
		})
	}
}

func TestRunUpgradesOnSchedule_CronWithSeconds(t *testing.T) {
	ctx := t.Context()

	updateCallCount := 0
	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ updateParams.Params) *metrics.Metric {
		updateCallCount++

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(context.Context, logging.StartupParams) {}

	// Use a 6-field cron spec that includes seconds (every 2 seconds)
	scheduleSpec := "*/2 * * * * *"

	// Use timeout to avoid hanging - should execute at least once within 5 seconds
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 5*time.Second)
	defer timeoutCancel()

	deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)
	deps.ScheduleSpec = scheduleSpec
	err := RunUpgradesOnSchedule(timeoutCtx, deps)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Should have executed at least once (depending on timing)
	if updateCallCount == 0 {
		t.Error("expected at least one update call")
	}
}

// TestRunUpgradesOnSchedule_ReplacedOlderInstance verifies that a process that
// replaced an older Watchtower makes no update at startup even with update on
// start set. Its startup message says so. Only its first scheduled run skips
// self-updates, with or without update on start. A process that replaced none
// updates at startup and never skips them.
func TestRunUpgradesOnSchedule_ReplacedOlderInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		replaced      bool
		updateOnStart bool
		// wantAnnounced is the update at startup the startup message announces.
		wantAnnounced bool
		// wantSkippedSelfUpdate is each run's SkipSelfUpdate in order, the
		// update at startup first when there is one.
		wantSkippedSelfUpdate []bool
	}{
		{
			name:                  "replaced an older instance",
			replaced:              true,
			updateOnStart:         true,
			wantAnnounced:         false,
			wantSkippedSelfUpdate: []bool{true, false},
		},
		{
			name:                  "replaced an older instance without update on start",
			replaced:              true,
			updateOnStart:         false,
			wantAnnounced:         false,
			wantSkippedSelfUpdate: []bool{true, false},
		},
		{
			name:                  "replaced none",
			replaced:              false,
			updateOnStart:         true,
			wantAnnounced:         true,
			wantSkippedSelfUpdate: []bool{false, false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				// The checks read these only after the scheduler has returned.
				var skippedSelfUpdate []bool

				var startup *logging.StartupParams

				deps := testDeps(t, func(_ context.Context, _ types.Filter, params updateParams.Params) *metrics.Metric {
					skippedSelfUpdate = append(skippedSelfUpdate, params.SkipSelfUpdate)

					return &metrics.Metric{}
				}, func(_ context.Context, written logging.StartupParams) {
					startup = &written
				})
				deps.UpdateOnStart = tt.updateOnStart
				deps.ScheduleSpec = "@every 1h"
				deps.ReplacedOlderInstance = tt.replaced

				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)

				go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

				// Two scheduled runs, at one and two hours.
				time.Sleep(2*time.Hour + time.Minute)
				synctest.Wait()

				cancel()
				require.NoError(t, <-done)

				require.NotNil(t, startup, "the startup message was not written")
				require.NotNil(t, startup.UpdateOnStart)
				assert.Equal(t, tt.wantAnnounced, *startup.UpdateOnStart, "the startup message announced the wrong update at startup")
				assert.Equal(t, tt.wantSkippedSelfUpdate, skippedSelfUpdate)
			})
		})
	}
}

func TestRunUpgradesOnSchedule_ScheduledRuns_Execution(t *testing.T) {
	ctx := t.Context()

	var executionTimes []time.Time

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ updateParams.Params) *metrics.Metric {
		executionTimes = append(executionTimes, time.Now())

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	writeStartupMessage := func(context.Context, logging.StartupParams) {}

	// Use a cron spec that runs every 1 second
	scheduleSpec := "*/1 * * * * *"

	startTime := time.Now()

	// Use timeout to allow multiple executions
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer timeoutCancel()

	deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)
	deps.ScheduleSpec = scheduleSpec
	err := RunUpgradesOnSchedule(timeoutCtx, deps)
	// Should complete without error when context times out (clean cancellation)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Should have executed multiple times
	if len(executionTimes) < 2 {
		t.Fatalf("expected at least 2 executions, got %d", len(executionTimes))
	}

	// Verify intervals are approximately correct (within 200ms tolerance for 1-second cron)
	for i := 1; i < len(executionTimes); i++ {
		interval := executionTimes[i].Sub(executionTimes[i-1])
		if interval < 800*time.Millisecond || interval > 1200*time.Millisecond {
			t.Errorf("execution interval %v is not within expected range [800ms, 1200ms]", interval)
		}
	}

	// Verify executions happened after start time
	for _, execTime := range executionTimes {
		if execTime.Before(startTime) {
			t.Errorf("execution time %v is before start time %v", execTime, startTime)
		}
	}
}

// TestRunUpgradesOnSchedule_KeepsBaseSkipSelfUpdate verifies that a run cannot
// enable self-updates that the base parameters disable.
func TestRunUpgradesOnSchedule_KeepsBaseSkipSelfUpdate(t *testing.T) {
	var capturedParams updateParams.Params

	runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params updateParams.Params) *metrics.Metric {
		capturedParams = params

		return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
	}

	timeoutCtx, timeoutCancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer timeoutCancel()

	// The run at startup does not skip self-updates on its own.
	deps := testDeps(t, runUpdatesWithNotifications, func(context.Context, logging.StartupParams) {})
	deps.UpdateOnStart = true
	deps.BaseParams.SkipSelfUpdate = true
	err := RunUpgradesOnSchedule(timeoutCtx, deps)
	require.NoError(t, err)

	assert.True(t, capturedParams.SkipSelfUpdate)
}

// TestRunUpgradesOnSchedule_EphemeralSelfUpdatePropagation verifies that the
// scheduler passes ephemeral self-update to the update function and leaves
// self-updates enabled, since ephemeral self-update chooses how Watchtower
// replaces itself rather than whether it does.
func TestRunUpgradesOnSchedule_EphemeralSelfUpdatePropagation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var captured atomic.Pointer[updateParams.Params]

		deps := testDeps(t, func(_ context.Context, _ types.Filter, params updateParams.Params) *metrics.Metric {
			captured.Store(&params)

			return &metrics.Metric{}
		}, func(context.Context, logging.StartupParams) {})
		deps.UpdateOnStart = true
		deps.BaseParams.EphemeralSelfUpdate = true

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)

		go func() { done <- RunUpgradesOnSchedule(ctx, deps) }()

		synctest.Wait()

		params := captured.Load()
		require.NotNil(t, params, "the update at startup did not run")
		assert.True(t, params.EphemeralSelfUpdate, "ephemeral self-update did not reach the update")
		assert.False(t, params.SkipSelfUpdate, "ephemeral self-update disabled self-updates")

		cancel()
		require.NoError(t, <-done)
	})
}

// TestRunUpgradesOnSchedule_ReviveStoppedPropagation verifies that RunUpgradesOnSchedule
// passes the ReviveStopped flag through [updateParams.Params] to the update function.
func TestRunUpgradesOnSchedule_ReviveStoppedPropagation(t *testing.T) {
	tests := []struct {
		name           string
		reviveStopped  bool
		expectSelected bool
	}{
		{
			name:           "reviveStopped=true",
			reviveStopped:  true,
			expectSelected: true,
		},
		{
			name:           "reviveStopped=false",
			reviveStopped:  false,
			expectSelected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			var capturedParams updateParams.Params

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params updateParams.Params) *metrics.Metric {
				capturedParams = params

				return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(context.Context, logging.StartupParams) {}

			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)
			deps.UpdateOnStart = true
			deps.BaseParams.ReviveStopped = tt.reviveStopped
			err := RunUpgradesOnSchedule(timeoutCtx, deps)
			require.NoError(t, err)

			assert.Equal(t, tt.expectSelected, capturedParams.ReviveStopped,
				"ReviveStopped should be %v in updateParams.Params", tt.expectSelected)
		})
	}
}

// TestRunUpgradesOnSchedule_UseComposeDependsOnPropagation verifies that
// RunUpgradesOnSchedule passes UseComposeDependsOn through [updateParams.Params] so
// scheduled cycles honor Compose depends_on the same way as --run-once.
func TestRunUpgradesOnSchedule_UseComposeDependsOnPropagation(t *testing.T) {
	tests := []struct {
		name                string
		useComposeDependsOn bool
		expectSelected      bool
	}{
		{
			name:                "useComposeDependsOn=true",
			useComposeDependsOn: true,
			expectSelected:      true,
		},
		{
			name:                "useComposeDependsOn=false",
			useComposeDependsOn: false,
			expectSelected:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			var capturedParams updateParams.Params

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params updateParams.Params) *metrics.Metric {
				capturedParams = params

				return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
			}

			writeStartupMessage := func(context.Context, logging.StartupParams) {}

			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
			defer timeoutCancel()

			deps := testDeps(t, runUpdatesWithNotifications, writeStartupMessage)
			deps.UpdateOnStart = true
			deps.BaseParams.UseComposeDependsOn = tt.useComposeDependsOn
			err := RunUpgradesOnSchedule(timeoutCtx, deps)
			require.NoError(t, err)

			assert.Equal(t, tt.expectSelected, capturedParams.UseComposeDependsOn,
				"UseComposeDependsOn should be %v in updateParams.Params", tt.expectSelected)
			assert.False(t, capturedParams.RunOnce,
				"scheduled updates must keep RunOnce=false")
		})
	}
}
