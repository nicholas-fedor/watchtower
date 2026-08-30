package run

import (
	"context"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

// startCase records a running case and publishes case_start.
//
// Parameters:
//   - ctx: Cancellation.
//   - item: Case vector.
//
// Returns:
//   - error: Persist failure.
func (s *sitting) startCase(ctx context.Context, item engine.Case) error {
	if s.req.Records != nil {
		err := s.req.Records.UpsertCase(ctx, store.Case{
			RunID:     s.runID,
			CaseID:    item.ID(),
			Status:    store.CaseRunning,
			Factors:   item.Factors,
			Expect:    mustJSON(item.Expect),
			StartedAt: nowUTC(),
		})
		if err != nil {
			return err
		}

		_, _ = s.req.Records.AppendEvent(ctx, store.Event{
			RunID:  s.runID,
			CaseID: item.ID(),
			Kind:   store.EventCaseStart,
		})
	}

	if s.req.OnCaseStart != nil {
		s.req.OnCaseStart(item.ID())
	}

	return nil
}

// finishCase records the terminal case row and count event.
//
// Parameters:
//   - ctx: Cancellation.
//   - item: Case vector.
//   - result: Scheduler result.
//
// Returns:
//   - engine.Result: The same result, unchanged.
func (s *sitting) finishCase(ctx context.Context, item engine.Case, result engine.Result) engine.Result {
	if s.req.Records == nil {
		return result
	}

	writeCtx, writeCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer writeCancel()
	ctx = writeCtx

	rec := store.Case{
		RunID:      s.runID,
		CaseID:     result.CaseID,
		Status:     engineStatus(result),
		Factors:    item.Factors,
		Expect:     mustJSON(item.Expect),
		Error:      result.Err,
		DurationMs: result.Duration,
		FinishedAt: nowUTC(),
	}
	if upsertErr := s.req.Records.UpsertCase(ctx, rec); upsertErr != nil {
		result.Status = "fail"
		result.Passed = false
		result.Err = upsertErr.Error()
		rec.Status = store.CaseFail
		rec.Error = upsertErr.Error()
	}
	_, _ = s.req.Records.AppendEvent(ctx, store.Event{
		RunID:   s.runID,
		CaseID:  result.CaseID,
		Kind:    store.EventCaseEnd,
		Payload: mustJSON(map[string]string{"status": result.Status}),
	})
	if bumpErr := s.bumpCounts(ctx, rec.Status); bumpErr != nil {
		result.Status = "fail"
		result.Passed = false
		result.Err = bumpErr.Error()
	}

	if s.req.OnCaseEnd != nil {
		s.req.OnCaseEnd(item.ID())
	}

	return result
}

// bumpCounts increments sitting pass/fail/skip counters.
//
// Parameters:
//   - ctx: Cancellation.
//   - status: Terminal case status.
//
// Returns:
//   - error: IncrementCounts failure.
func (s *sitting) bumpCounts(ctx context.Context, status store.CaseStatus) error {
	var passed, failed, skipped int

	switch status {
	case store.CasePass:
		passed = 1
	case store.CaseFail:
		failed = 1
	case store.CaseSkip:
		skipped = 1
	case store.CasePending, store.CaseRunning, store.CaseInterrupted:
		return nil
	}

	latest, err := s.req.Records.IncrementCounts(ctx, s.runID, passed, failed, skipped)
	if err != nil {
		return err
	}

	_, _ = s.req.Records.AppendEvent(ctx, store.Event{
		RunID:   s.runID,
		Kind:    store.EventCounts,
		Payload: mustJSON(countsPayload{Passed: latest.Passed, Failed: latest.Failed, Skipped: latest.Skipped}),
	})

	return nil
}
