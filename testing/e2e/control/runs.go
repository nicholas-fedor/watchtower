package control

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

// CreateRun queues a sitting and returns it.
//
// Parameters:
//   - ctx: Cancellation.
//   - spec: Operator input.
//   - label: Human stamp stored beside the UUID.
//
// Returns:
//   - store.Run: Queued sitting.
//   - error: Persist failure.
func (s *Service) CreateRun(ctx context.Context, spec store.Spec, label string) (store.Run, error) {
	spec.Generator = cmp.Or(spec.Generator, "product")
	spec.Seed = cmp.Or(spec.Seed, int64(1))

	run := store.Run{
		ID:        uuid.NewV7().String(),
		Label:     label,
		CreatedAt: time.Now().UTC(),
		Status:    store.RunQueued,
		Spec:      spec,
	}

	err := s.store.CreateRun(ctx, run)
	if err != nil {
		return store.Run{}, err
	}

	_, _ = s.store.AppendEvent(ctx, store.Event{
		RunID:   run.ID,
		Kind:    store.EventRunStatus,
		Payload: mustStatusJSON(store.RunQueued),
	})

	return run, nil
}

// GetRun returns a sitting with live current case IDs.
//
// Parameters:
//   - ctx: Cancellation.
//   - id: Run UUID.
//
// Returns:
//   - store.Run: Snapshot.
//   - error: ErrNotFound or store failure.
func (s *Service) GetRun(ctx context.Context, id string) (store.Run, error) {
	run, err := s.store.GetRun(ctx, id)
	if err != nil {
		return store.Run{}, err
	}

	s.mu.Lock()
	if s.currentID == id {
		run.CurrentIDs = slices.Clone(s.currentIDs)
	}
	s.mu.Unlock()

	return run, nil
}

// ListRuns proxies the store.
//
// Parameters:
//   - ctx: Cancellation.
//   - filter: Status and pagination.
//
// Returns:
//   - []store.Run: Page of sittings.
//   - error: Store failure.
func (s *Service) ListRuns(ctx context.Context, filter store.RunListFilter) ([]store.Run, error) {
	return s.store.ListRuns(ctx, filter)
}

// Cancel stops a queued or running sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - id: Run UUID.
//
// Returns:
//   - store.Run: Canceled snapshot.
//   - error: ErrConflict when the run is already terminal.
func (s *Service) Cancel(ctx context.Context, id string) (store.Run, error) {
	run, err := s.store.GetRun(ctx, id)
	if err != nil {
		return store.Run{}, err
	}

	switch run.Status {
	case store.RunCompleted, store.RunFailed, store.RunCanceled:
		return store.Run{}, fmt.Errorf("%w: run %s is %s", store.ErrConflict, id, run.Status)
	case store.RunQueued, store.RunRunning, store.RunInterrupted:
	}

	s.mu.Lock()
	if s.currentID == id && s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	run.Status = store.RunCanceled
	run.FinishedAt = time.Now().UTC()

	updErr := s.store.TransitionRun(ctx, run, store.RunQueued, store.RunRunning, store.RunInterrupted)
	if updErr != nil {
		return store.Run{}, updErr
	}

	_, _ = s.store.AppendEvent(ctx, store.Event{
		RunID:   id,
		Kind:    store.EventRunStatus,
		Payload: mustStatusJSON(store.RunCanceled),
	})

	return run, nil
}

// Resume queues an interrupted or canceled sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - id: Run UUID.
//
// Returns:
//   - store.Run: Re-queued snapshot.
//   - error: ErrConflict when the run is running or completed.
func (s *Service) Resume(ctx context.Context, id string) (store.Run, error) {
	run, err := s.store.GetRun(ctx, id)
	if err != nil {
		return store.Run{}, err
	}

	switch run.Status {
	case store.RunInterrupted, store.RunCanceled, store.RunQueued:
	case store.RunRunning, store.RunCompleted, store.RunFailed:
		return store.Run{}, fmt.Errorf("%w: run %s is %s", store.ErrConflict, id, run.Status)
	}

	run.Status = store.RunQueued
	run.FinishedAt = time.Time{}
	run.Error = ""

	updErr := s.store.TransitionRun(ctx, run, store.RunInterrupted, store.RunCanceled, store.RunQueued)
	if updErr != nil {
		return store.Run{}, updErr
	}

	_, _ = s.store.AppendEvent(ctx, store.Event{
		RunID:   id,
		Kind:    store.EventRunStatus,
		Payload: mustStatusJSON(store.RunQueued),
	})

	return run, nil
}
