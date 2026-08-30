package control

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

// loopTick is how often the dispatcher looks for a queued sitting.
const loopTick = 200 * time.Millisecond

// InterruptRunning marks leftover running rows after a process restart.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Store failure.
func (s *Service) InterruptRunning(ctx context.Context) error {
	return s.store.InterruptRunning(ctx)
}

// Loop dequeues sittings until ctx is done.
//
// Parameters:
//   - ctx: Shutdown signal.
func (s *Service) Loop(ctx context.Context) {
	ticker := time.NewTicker(loopTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pump(ctx)
		}
	}
}

// pump starts the next queued sitting when the execution slot is free.
//
// Parameters:
//   - parent: Dispatcher context.
func (s *Service) pump(parent context.Context) {
	s.mu.Lock()
	busy := s.currentID != ""
	s.mu.Unlock()

	if busy {
		return
	}

	run, err := s.store.NextQueued(parent)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.setLoopErr(err)
		}

		return
	}

	s.setLoopErr(nil)

	runCtx, cancel := context.WithCancel(parent)

	s.mu.Lock()
	s.currentID = run.ID
	s.cancel = cancel
	s.mu.Unlock()

	latest, getErr := s.store.GetRun(parent, run.ID)
	if getErr != nil {
		cancel()
		s.abandonClaim(parent, run, getErr)
		s.clearCurrent(run.ID)

		return
	}

	if latest.Status == store.RunCanceled {
		cancel()
		s.clearCurrent(run.ID)

		return
	}

	_, _ = s.store.AppendEvent(parent, store.Event{
		RunID:   run.ID,
		Kind:    store.EventRunStatus,
		Payload: json.RawMessage(`{"status":"running"}`),
	})

	execErr := s.exec(runCtx, s, run)
	s.finishPump(parent, run, runCtx, cancel, execErr)
}

// finishPump writes the terminal sitting status and releases the slot.
//
// Parameters:
//   - parent: Dispatcher context.
//   - run: Sitting that was executing.
//   - runCtx: Per-sitting cancel context.
//   - cancel: Cancels runCtx.
//   - execErr: Error from ExecuteFunc, or nil.
func (s *Service) finishPump(parent context.Context, run store.Run, runCtx context.Context, cancel context.CancelFunc, execErr error) {
	defer cancel()
	defer s.clearCurrent(run.ID)

	writeCtx, writeCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer writeCancel()

	latest, getErr := s.store.GetRun(writeCtx, run.ID)
	if getErr != nil {
		s.abandonClaim(parent, run, getErr)

		return
	}

	if latest.Status == store.RunCanceled {
		return
	}

	latest.FinishedAt = time.Now().UTC()
	switch {
	case runCtx.Err() != nil:
		latest.Status = store.RunInterrupted
		if execErr != nil {
			latest.Error = execErr.Error()
		}
	case execErr != nil:
		latest.Status = store.RunFailed
		latest.Error = execErr.Error()
	default:
		latest.Status = store.RunCompleted
		latest.Error = ""
	}

	if updErr := s.store.TransitionRun(writeCtx, latest, store.RunRunning); updErr != nil {
		if !errors.Is(updErr, store.ErrConflict) {
			s.abandonClaim(parent, run, updErr)
		}

		return
	}

	_, _ = s.store.AppendEvent(writeCtx, store.Event{
		RunID:   run.ID,
		Kind:    store.EventRunStatus,
		Payload: mustStatusJSON(latest.Status),
	})
}

// mustStatusJSON encodes a run_status event payload.
//
// Parameters:
//   - status: Sitting status.
//
// Returns:
//   - json.RawMessage: {"status":"..."} or {}.
func mustStatusJSON(status store.RunStatus) json.RawMessage {
	raw, err := jsonv2.Marshal(map[string]string{"status": string(status)})
	if err != nil {
		return json.RawMessage(`{}`)
	}

	return raw
}

// abandonClaim marks a claimed sitting interrupted when it cannot be executed.
//
// Parameters:
//   - parent: Dispatcher context.
//   - run: Sitting already claimed as running.
//   - cause: Why execution did not start.
func (s *Service) abandonClaim(parent context.Context, run store.Run, cause error) {
	writeCtx, writeCancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer writeCancel()

	s.setLoopErr(cause)

	run.Status = store.RunInterrupted
	run.FinishedAt = time.Now().UTC()
	if cause != nil {
		run.Error = cause.Error()
	}

	if err := s.store.TransitionRun(writeCtx, run, store.RunRunning); err != nil && !errors.Is(err, store.ErrConflict) {
		s.setLoopErr(err)
	}
}
