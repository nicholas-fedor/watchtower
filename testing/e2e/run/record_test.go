package run

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

type failIncStore struct {
	*store.Memory
}

func (f *failIncStore) IncrementCounts(context.Context, string, int, int, int) (store.Run, error) {
	return store.Run{}, errors.New("inc boom")
}

func TestBumpCountsIncrementsPerStatus(t *testing.T) {
	ctx := t.Context()
	mem := store.NewMemory()
	require.NoError(t, mem.CreateRun(ctx, store.Run{ID: "run", Status: store.RunRunning}))
	s := &sitting{req: SittingRequest{Records: mem}, runID: "run"}

	require.NoError(t, s.bumpCounts(ctx, store.CasePass))
	require.NoError(t, s.bumpCounts(ctx, store.CaseFail))
	require.NoError(t, s.bumpCounts(ctx, store.CaseSkip))
	require.NoError(t, s.bumpCounts(ctx, store.CaseRunning))

	got, err := mem.GetRun(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, 1, got.Passed)
	require.Equal(t, 1, got.Failed)
	require.Equal(t, 1, got.Skipped)
	require.Equal(t, store.RunRunning, got.Status)
}

func TestBumpCountsParallelNoLostUpdates(t *testing.T) {
	ctx := t.Context()
	mem := store.NewMemory()
	require.NoError(t, mem.CreateRun(ctx, store.Run{ID: "run", Status: store.RunRunning}))
	s := &sitting{req: SittingRequest{Records: mem}, runID: "run"}

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			require.NoError(t, s.bumpCounts(ctx, store.CasePass))
		})
	}
	wg.Wait()

	got, err := mem.GetRun(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, 50, got.Passed)
}

func TestFinishCaseFailsWhenIncrementCountsFails(t *testing.T) {
	ctx := t.Context()
	mem := &failIncStore{Memory: store.NewMemory()}
	require.NoError(t, mem.CreateRun(ctx, store.Run{ID: "run", Status: store.RunRunning}))
	s := &sitting{req: SittingRequest{Records: mem}, runID: "run"}

	result := s.finishCase(ctx, engine.Case{}, engine.Result{CaseID: "c", Status: "pass", Passed: true})
	require.Equal(t, "fail", result.Status)
	require.Contains(t, result.Err, "inc boom")
}
