package run

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
	"github.com/nicholas-fedor/watchtower/testing/e2e/host"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

// Controller is the control-plane surface ExecuteStored needs.
type Controller interface {
	Store() store.Store
	Logs() stream.Logs
	SetPool(size, busy, idle int)
	AddCurrentCase(runID, caseID string)
	RemoveCurrentCase(runID, caseID string)
}

// skipSet adapts completed IDs to the scheduler skip seam.
type skipSet map[string]store.CaseStatus

// Has reports whether the case already finished.
//
// Parameters:
//   - caseID: Case identifier.
//
// Returns:
//   - bool: True when the ID is in the set.
func (s skipSet) Has(caseID string) bool {
	_, ok := s[caseID]

	return ok
}

// ExecuteStored runs a queued sitting against DinD and records into the store.
//
// Parameters:
//   - ctx: Cancellation.
//   - svc: Control-plane surface (store, logs, live pool).
//   - run: Queued or resumed sitting.
//
// Returns:
//   - error: Setup or case failure.
func ExecuteStored(ctx context.Context, svc Controller, run store.Run) error {
	snap, discErr := host.Discover("/")
	if discErr != nil {
		return fmt.Errorf("discover host: %w", discErr)
	}

	workers := host.ResolveWorkers(run.Spec.Workers, snap.RecommendedWorkers)
	bound, boundErr := engine.WorkBound(run.Spec.Generator, run.Spec.FilePath, run.Spec.Limit)
	if boundErr != nil {
		return boundErr
	}

	workers = host.CapWorkers(workers, bound)
	if setErr := svc.Store().SetRunWorkers(ctx, run.ID, workers); setErr != nil {
		return setErr
	}

	completed, compErr := svc.Store().CompletedIDs(ctx, run.ID)
	if compErr != nil {
		return compErr
	}

	req := SittingRequest{
		Workers:   workers,
		Shard:     run.Spec.Shard,
		Offset:    run.Spec.Offset,
		Limit:     run.Spec.Limit,
		Generator: run.Spec.Generator,
		Seed:      run.Spec.Seed,
		Filter:    run.Spec.Filter,
		Topic:     run.Spec.Topic,
		Keep:      run.Spec.Keep,
		FilePath:  run.Spec.FilePath,
		RunID:     run.ID,
		Records:   svc.Store(),
		Logs:      svc.Logs(),
		Skip:      skipSet(completed),
		OnPool: func(busy, idle int) {
			svc.SetPool(workers, busy, idle)
		},
		OnCaseStart: func(id string) {
			svc.AddCurrentCase(run.ID, id)
		},
		OnCaseEnd: func(id string) {
			svc.RemoveCurrentCase(run.ID, id)
		},
	}

	_, err := Sitting(ctx, req)

	return err
}

// countsPayload is a compact SSE document.
type countsPayload struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

// mustJSON marshals v, falling back to {}.
//
// Parameters:
//   - v: Value to encode.
//
// Returns:
//   - json.RawMessage: JSON bytes.
func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}

	return raw
}

// nowUTC returns the current UTC time.
//
// Returns:
//   - time.Time: Now in UTC.
func nowUTC() time.Time {
	return time.Now().UTC()
}

// engineStatus maps a scheduler result status onto a store case status.
//
// Parameters:
//   - result: Scheduler result.
//
// Returns:
//   - store.CaseStatus: pass, fail, skip, or interrupted.
func engineStatus(result engine.Result) store.CaseStatus {
	switch result.Status {
	case "pass":
		return store.CasePass
	case "fail":
		return store.CaseFail
	case "skip":
		return store.CaseSkip
	case "interrupted":
		return store.CaseInterrupted
	default:
		if result.Passed {
			return store.CasePass
		}

		return store.CaseFail
	}
}
