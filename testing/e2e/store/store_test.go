package store

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryCreateGetList(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()

	first := Run{
		ID:        "run-a",
		Label:     "a",
		CreatedAt: time.Date(2026, 8, 29, 1, 0, 0, 0, time.UTC),
		Status:    RunQueued,
		Spec:      Spec{Generator: "product", Topic: "cleanup"},
	}
	second := Run{
		ID:        "run-b",
		Label:     "b",
		CreatedAt: time.Date(2026, 8, 29, 2, 0, 0, 0, time.UTC),
		Status:    RunRunning,
		Spec:      Spec{Generator: "file"},
	}

	require.NoError(t, mem.CreateRun(ctx, first))
	require.NoError(t, mem.CreateRun(ctx, second))
	require.ErrorIs(t, mem.CreateRun(ctx, first), ErrConflict)

	got, err := mem.GetRun(ctx, "run-a")
	require.NoError(t, err)
	require.Equal(t, "cleanup", got.Spec.Topic)

	_, err = mem.GetRun(ctx, "missing")
	require.ErrorIs(t, err, ErrNotFound)

	listed, err := mem.ListRuns(ctx, RunListFilter{})
	require.NoError(t, err)
	require.Equal(t, []string{"run-b", "run-a"}, []string{listed[0].ID, listed[1].ID})

	queued, err := mem.ListRuns(ctx, RunListFilter{Status: RunQueued})
	require.NoError(t, err)
	require.Len(t, queued, 1)
	require.Equal(t, "run-a", queued[0].ID)
}

func TestMemoryNextQueuedClaimsOldest(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()

	require.NoError(t, mem.CreateRun(ctx, Run{
		ID: "q1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Status: RunQueued,
	}))
	require.NoError(t, mem.CreateRun(ctx, Run{
		ID: "q2", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Status: RunQueued,
	}))

	next, err := mem.NextQueued(ctx)
	require.NoError(t, err)
	require.Equal(t, "q1", next.ID)
	require.Equal(t, RunRunning, next.Status)

	got, err := mem.GetRun(ctx, "q1")
	require.NoError(t, err)
	require.Equal(t, RunRunning, got.Status)
	require.False(t, got.StartedAt.IsZero())

	_, err = mem.NextQueued(ctx)
	require.ErrorIs(t, err, ErrNotFound)

	got.Status = RunCompleted
	require.NoError(t, mem.UpdateRunStatus(ctx, got))

	second, err := mem.NextQueued(ctx)
	require.NoError(t, err)
	require.Equal(t, "q2", second.ID)
}

func TestMemoryNextQueuedBlockedByRunning(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()

	require.NoError(t, mem.CreateRun(ctx, Run{
		ID: "r1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Status: RunRunning,
	}))
	require.NoError(t, mem.CreateRun(ctx, Run{
		ID: "q1", CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Status: RunQueued,
	}))

	_, err := mem.NextQueued(ctx)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryNextQueuedConcurrentClaim(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()
	require.NoError(t, mem.CreateRun(ctx, Run{
		ID: "q1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Status: RunQueued,
	}))

	ids := make(chan string, 2)
	errs := make(chan error, 2)

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			run, err := mem.NextQueued(ctx)
			if err != nil {
				errs <- err

				return
			}

			ids <- run.ID
		})
	}
	wg.Wait()
	close(ids)
	close(errs)

	var claimed []string
	for id := range ids {
		claimed = append(claimed, id)
	}

	require.Equal(t, []string{"q1"}, claimed)
	require.ErrorIs(t, <-errs, ErrNotFound)
}

func TestMemoryIncrementCountsParallel(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()
	require.NoError(t, mem.CreateRun(ctx, Run{ID: "run", Status: RunRunning}))

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			_, err := mem.IncrementCounts(ctx, "run", 1, 0, 0)
			require.NoError(t, err)
		})
	}
	wg.Wait()

	got, err := mem.GetRun(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, 50, got.Passed)
}

func TestMemoryInterruptRunning(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()

	require.NoError(t, mem.CreateRun(ctx, Run{
		ID: "r1", CreatedAt: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Status: RunRunning,
	}))
	require.NoError(t, mem.UpsertCase(ctx, Case{RunID: "r1", CaseID: "c1", Status: CaseRunning}))
	require.NoError(t, mem.InterruptRunning(ctx))

	run, err := mem.GetRun(ctx, "r1")
	require.NoError(t, err)
	require.Equal(t, RunInterrupted, run.Status)

	item, err := mem.GetCase(ctx, "r1", "c1")
	require.NoError(t, err)
	require.Equal(t, CaseInterrupted, item.Status)

	_, err = NewMemory().NextQueued(ctx)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryCloneIsolation(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()
	require.NoError(t, mem.CreateRun(ctx, Run{ID: "run", Status: RunRunning}))
	require.NoError(t, mem.UpsertCase(ctx, Case{
		RunID: "run", CaseID: "c1", Status: CasePass, Factors: map[string]string{"k": "v"},
	}))

	got, err := mem.GetCase(ctx, "run", "c1")
	require.NoError(t, err)
	got.Factors["k"] = "mutated"

	again, err := mem.GetCase(ctx, "run", "c1")
	require.NoError(t, err)
	require.Equal(t, "v", again.Factors["k"])
}

func TestPostgresUpsertPreservesEmptyFields(t *testing.T) {
	dsn := os.Getenv("WATCHTOWER_E2E_DATABASE_URL")
	pg, err := OpenPostgres(t.Context(), dsn)
	if err != nil {
		t.Skip("postgres:", err)
	}
	t.Cleanup(func() { _ = pg.Close() })

	ctx := t.Context()
	id := "pg-" + t.Name()
	require.NoError(t, pg.CreateRun(ctx, Run{ID: id, Status: RunRunning}))
	t.Cleanup(func() {
		_, _ = pg.pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, id)
	})

	require.NoError(t, pg.UpsertCase(ctx, Case{
		RunID:       id,
		CaseID:      "alpha",
		Status:      CaseRunning,
		Factors:     map[string]string{"topic": "cleanup"},
		Expect:      json.RawMessage(`{"outcome":"updated"}`),
		Argv:        []string{"watchtower"},
		Env:         map[string]string{"A": "1"},
		Error:       "hold",
		HTTPDetails: "body",
	}))
	require.NoError(t, pg.UpsertCase(ctx, Case{RunID: id, CaseID: "alpha", Status: CasePass}))

	got, err := pg.GetCase(ctx, id, "alpha")
	require.NoError(t, err)
	require.Equal(t, CasePass, got.Status)
	require.Equal(t, "cleanup", got.Factors["topic"])
	require.JSONEq(t, `{"outcome":"updated"}`, string(got.Expect))
	require.Equal(t, []string{"watchtower"}, got.Argv)
	require.Equal(t, "1", got.Env["A"])
	require.Equal(t, "hold", got.Error)
	require.Equal(t, "body", got.HTTPDetails)
}

func TestOpenPostgresEmptyDSN(t *testing.T) {
	_, err := OpenPostgres(t.Context(), "")
	require.ErrorIs(t, err, ErrNotConfigured)
}

func TestMemoryCasesAndEvents(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()
	require.NoError(t, mem.CreateRun(ctx, Run{ID: "run", Status: RunRunning}))

	require.NoError(t, mem.UpsertCase(ctx, Case{
		RunID:   "run",
		CaseID:  "alpha",
		Status:  CasePass,
		Factors: map[string]string{"topic": "cleanup", "flag.cleanup": "true"},
		Expect:  json.RawMessage(`{"outcome":"updated"}`),
	}))
	require.NoError(t, mem.UpsertCase(ctx, Case{RunID: "run", CaseID: "beta", Status: CaseFail, Error: "boom"}))
	require.NoError(t, mem.UpsertCase(ctx, Case{RunID: "run", CaseID: "gamma", Status: CaseSkip}))

	failed, total, err := mem.ListCases(ctx, "run", CaseListFilter{Status: CaseFail})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "beta", failed[0].CaseID)

	hits, total, err := mem.ListCases(ctx, "run", CaseListFilter{Query: "cleanup"})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, "alpha", hits[0].CaseID)

	done, err := mem.CompletedIDs(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, CasePass, done["alpha"])
	require.Equal(t, CaseFail, done["beta"])
	require.Equal(t, CaseSkip, done["gamma"])

	ev1, err := mem.AppendEvent(ctx, Event{RunID: "run", Kind: EventCaseStart, CaseID: "alpha"})
	require.NoError(t, err)
	require.Equal(t, int64(1), ev1.ID)

	_, err = mem.AppendEvent(ctx, Event{RunID: "run", Kind: EventCaseEnd, CaseID: "alpha"})
	require.NoError(t, err)

	page, err := mem.ListEvents(ctx, "run", 1, 10)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, EventCaseEnd, page[0].Kind)

	_, err = mem.GetCase(ctx, "run", "nope")
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, mem.UpsertCase(ctx, Case{RunID: "run", CaseID: "alpha", InspectBefore: json.RawMessage(`{"id":"1"}`)}))
	merged, err := mem.GetCase(ctx, "run", "alpha")
	require.NoError(t, err)
	require.Equal(t, CasePass, merged.Status)
	require.JSONEq(t, `{"id":"1"}`, string(merged.InspectBefore))
	require.Equal(t, "cleanup", merged.Factors["topic"])
}

func TestPageLimit(t *testing.T) {
	require.Equal(t, 50, pageLimit(0))
	require.Equal(t, 10, pageLimit(10))
	require.Equal(t, 200, pageLimit(999))
}

func TestMemoryTransitionRunConflict(t *testing.T) {
	ctx := t.Context()
	mem := NewMemory()
	require.NoError(t, mem.CreateRun(ctx, Run{ID: "run", Status: RunRunning}))

	require.NoError(t, mem.TransitionRun(ctx, Run{ID: "run", Status: RunCompleted}, RunRunning))
	got, err := mem.GetRun(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, RunCompleted, got.Status)

	err = mem.TransitionRun(ctx, Run{ID: "run", Status: RunCanceled}, RunRunning)
	require.ErrorIs(t, err, ErrConflict)
	got, err = mem.GetRun(ctx, "run")
	require.NoError(t, err)
	require.Equal(t, RunCompleted, got.Status)
}

func TestLikePatternEscapesWildcards(t *testing.T) {
	require.Equal(t, `%foo\%bar\_baz\\%`, likePattern(`foo%bar_baz\`))
}
