package control

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

type failGetStore struct {
	*store.Memory
	failGet atomic.Bool
}

func (f *failGetStore) GetRun(ctx context.Context, id string) (store.Run, error) {
	if f.failGet.Load() {
		return store.Run{}, errors.New("get boom")
	}

	return f.Memory.GetRun(ctx, id)
}

type claimHookStore struct {
	*store.Memory
	afterClaim func(id string)
}

func (c *claimHookStore) NextQueued(ctx context.Context) (store.Run, error) {
	run, err := c.Memory.NextQueued(ctx)
	if err == nil && c.afterClaim != nil {
		c.afterClaim(run.ID)
	}

	return run, err
}

func TestRequestStopClosesStopping(t *testing.T) {
	svc := New(store.NewMemory(), stream.NewMemory(), nil)
	select {
	case <-svc.Stopping():
		t.Fatal("stop already closed")
	default:
	}

	svc.RequestStop()
	svc.RequestStop()
	<-svc.Stopping()
}

func TestCreateAndCancelQueued(t *testing.T) {
	svc := New(store.NewMemory(), stream.NewMemory(), nil)

	run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "product", Topic: "cleanup"}, "label")
	require.NoError(t, err)
	require.Equal(t, store.RunQueued, run.Status)
	require.NotEmpty(t, run.ID)

	canceled, err := svc.Cancel(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, store.RunCanceled, canceled.Status)

	_, err = svc.Cancel(t.Context(), run.ID)
	require.ErrorIs(t, err, store.ErrConflict)
}

func TestCreateRunDefaults(t *testing.T) {
	svc := New(store.NewMemory(), stream.NewMemory(), nil)

	run, err := svc.CreateRun(t.Context(), store.Spec{}, "label")
	require.NoError(t, err)
	require.Equal(t, "product", run.Spec.Generator)
	require.Equal(t, int64(1), run.Spec.Seed)
}

func TestResumeInterruptedAndConflict(t *testing.T) {
	svc := New(store.NewMemory(), stream.NewMemory(), nil)
	ctx := t.Context()

	run, err := svc.CreateRun(ctx, store.Spec{Generator: "file"}, "x")
	require.NoError(t, err)
	run.Status = store.RunInterrupted
	require.NoError(t, svc.Store().UpdateRunStatus(ctx, run))

	queued, err := svc.Resume(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, store.RunQueued, queued.Status)
	require.True(t, queued.FinishedAt.IsZero())

	run.Status = store.RunCompleted
	require.NoError(t, svc.Store().UpdateRunStatus(ctx, run))
	_, err = svc.Resume(ctx, run.ID)
	require.ErrorIs(t, err, store.ErrConflict)
}

func TestLoopCancelsWithoutExec(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		svc := New(store.NewMemory(), stream.NewMemory(), func(context.Context, *Service, store.Run) error {
			calls.Add(1)

			return nil
		})
		_, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "x")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			svc.Loop(ctx)
			close(done)
		}()
		cancel()
		<-done
		require.Equal(t, int32(0), calls.Load())
	})
}

func TestPumpClaimsNextAfterSlotFrees(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		records := store.NewMemory()
		ids := make(chan string, 2)
		svc := New(records, stream.NewMemory(), func(_ context.Context, _ *Service, run store.Run) error {
			ids <- run.ID

			return nil
		})

		first, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "a")
		require.NoError(t, err)
		second, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "b")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()
		require.Equal(t, first.ID, <-ids)

		synctest.Sleep(loopTick)
		synctest.Wait()
		require.Equal(t, second.ID, <-ids)

		got, err := svc.GetRun(t.Context(), first.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunCompleted, got.Status)
	})
}

func TestPumpSkipsWhenBusy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var started atomic.Int32
		svc := New(store.NewMemory(), stream.NewMemory(), func(ctx context.Context, _ *Service, _ store.Run) error {
			started.Add(1)
			<-ctx.Done()

			return ctx.Err()
		})

		first, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "a")
		require.NoError(t, err)
		second, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "b")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()
		require.Equal(t, int32(1), started.Load())

		synctest.Sleep(2 * loopTick)
		synctest.Wait()
		require.Equal(t, int32(1), started.Load())

		got, err := svc.GetRun(t.Context(), second.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunQueued, got.Status)

		_ = first
	})
}

func TestFinishPumpCanceledStaysCanceled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		svc := New(store.NewMemory(), stream.NewMemory(), func(ctx context.Context, _ *Service, _ store.Run) error {
			close(started)
			<-ctx.Done()

			return ctx.Err()
		})

		run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "x")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()
		<-started

		canceled, err := svc.Cancel(t.Context(), run.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunCanceled, canceled.Status)

		synctest.Wait()
		got, err := svc.GetRun(t.Context(), run.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunCanceled, got.Status)
	})
}

func TestFinishPumpDoesNotUncancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		svc := New(store.NewMemory(), stream.NewMemory(), func(ctx context.Context, _ *Service, _ store.Run) error {
			close(started)
			<-ctx.Done()

			return nil
		})

		run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "x")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()
		<-started

		_, err = svc.Cancel(t.Context(), run.ID)
		require.NoError(t, err)

		synctest.Wait()
		got, err := svc.GetRun(t.Context(), run.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunCanceled, got.Status)
	})
}

func TestAddCurrentCaseKeepsBoth(t *testing.T) {
	svc := New(store.NewMemory(), stream.NewMemory(), nil)
	svc.currentID = "run"
	svc.AddCurrentCase("run", "c1")
	svc.AddCurrentCase("run", "c2")
	svc.RemoveCurrentCase("run", "c1")

	run, err := svc.GetRun(t.Context(), "missing")
	require.Error(t, err)
	_ = run

	svc.mu.Lock()
	require.Equal(t, []string{"c2"}, svc.currentIDs)
	svc.mu.Unlock()
}

func TestPumpAbandonsClaimWhenGetRunFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		records := &failGetStore{Memory: store.NewMemory()}
		records.failGet.Store(true)
		var execs atomic.Int32
		svc := New(records, stream.NewMemory(), func(context.Context, *Service, store.Run) error {
			execs.Add(1)

			return nil
		})

		first, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "a")
		require.NoError(t, err)
		second, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "b")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()
		records.failGet.Store(false)

		got, err := svc.GetRun(t.Context(), first.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunInterrupted, got.Status)
		require.NotNil(t, svc.LoopErr())
		require.Equal(t, int32(0), execs.Load())

		synctest.Sleep(loopTick)
		synctest.Wait()
		got, err = svc.GetRun(t.Context(), second.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunCompleted, got.Status)
		require.Equal(t, int32(1), execs.Load())
	})
}

func TestPumpLeavesCanceledAfterClaim(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		records := &claimHookStore{Memory: store.NewMemory()}
		var svc *Service
		svc = New(records, stream.NewMemory(), func(context.Context, *Service, store.Run) error {
			t.Fatal("exec should not run")

			return nil
		})
		records.afterClaim = func(id string) {
			_, _ = svc.Cancel(t.Context(), id)
		}

		run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "x")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()

		got, err := svc.GetRun(t.Context(), run.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunCanceled, got.Status)
	})
}

func TestFinishPumpCaseFailuresAreCompleted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		records := store.NewMemory()
		svc := New(records, stream.NewMemory(), func(ctx context.Context, _ *Service, run store.Run) error {
			_, err := records.IncrementCounts(ctx, run.ID, 2, 18, 0)

			return err
		})
		run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "x")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()

		got, err := svc.GetRun(t.Context(), run.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunCompleted, got.Status)
		require.Equal(t, 2, got.Passed)
		require.Equal(t, 18, got.Failed)
		require.Empty(t, got.Error)
	})
}

func TestFinishPumpExecErrorIsFailed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		svc := New(store.NewMemory(), stream.NewMemory(), func(context.Context, *Service, store.Run) error {
			return errors.New("boom")
		})
		run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "x")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()

		got, err := svc.GetRun(t.Context(), run.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunFailed, got.Status)
		require.Contains(t, got.Error, "boom")
	})
}

func TestLoopCancelMarksInterrupted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{})
		svc := New(store.NewMemory(), stream.NewMemory(), func(ctx context.Context, _ *Service, _ store.Run) error {
			close(started)
			<-ctx.Done()

			return ctx.Err()
		})
		run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file"}, "x")
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		go svc.Loop(ctx)

		synctest.Sleep(loopTick)
		synctest.Wait()
		<-started
		cancel()
		synctest.Wait()

		got, err := svc.GetRun(t.Context(), run.ID)
		require.NoError(t, err)
		require.Equal(t, store.RunInterrupted, got.Status)
	})
}

func TestSkipCompleted(t *testing.T) {
	skip := SkipCompleted{"a": store.CasePass}
	require.True(t, skip.Has("a"))
	require.False(t, skip.Has("b"))
}

func TestHostSnapshotFillsPool(t *testing.T) {
	svc := New(store.NewMemory(), stream.NewMemory(), nil)
	svc.SetPool(3, 1, 2)

	snap, err := svc.HostSnapshot("/")
	require.NoError(t, err)
	require.Equal(t, 3, snap.PoolSize)
	require.Equal(t, 1, snap.BusyWorkers)
	require.Equal(t, 2, snap.IdleWorkers)
}
