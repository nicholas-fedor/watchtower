package control

import (
	"context"
	"slices"
	"sync"

	"github.com/nicholas-fedor/watchtower/testing/e2e/host"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

// ExecuteFunc runs one sitting against DinD. Tests inject a fake.
type ExecuteFunc func(ctx context.Context, svc *Service, run store.Run) error

// Service is the control-plane state machine.
type Service struct {
	mu sync.Mutex
	// store is the durable run/case/event record.
	store store.Store
	// logs is Watchtower stdout/stderr.
	logs stream.Logs
	// exec runs one sitting against DinD.
	exec ExecuteFunc
	// currentID is the sitting occupying the execution slot.
	currentID string
	// currentIDs are case IDs in flight on currentID.
	currentIDs []string
	// busy is workers currently executing a case.
	busy int
	// idle is started workers waiting for a case.
	idle int
	// poolSize is busy plus idle.
	poolSize int
	// cancel stops the sitting in the execution slot.
	cancel context.CancelFunc
	// loopErr is the last non-ErrNotFound dispatcher error.
	loopErr error
	// stop is closed when Listen is shutting down.
	stop chan struct{}
	// stopOnce closes stop once.
	stopOnce sync.Once
}

// New constructs a Service.
//
// Parameters:
//   - records: Run/case store.
//   - logs: Log streams.
//   - exec: Sitting runner. Nil means no-op (API-only tests).
//
// Returns:
//   - *Service: Ready controller.
func New(records store.Store, logs stream.Logs, exec ExecuteFunc) *Service {
	if exec == nil {
		exec = func(context.Context, *Service, store.Run) error { return nil }
	}

	return &Service{store: records, logs: logs, exec: exec, stop: make(chan struct{})}
}

// RequestStop closes Stopping. Idempotent.
func (s *Service) RequestStop() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
}

// Stopping is closed when the control plane is shutting down.
//
// Returns:
//   - <-chan struct{}: Closed on RequestStop.
func (s *Service) Stopping() <-chan struct{} {
	return s.stop
}

// Store returns the run store.
//
// Returns:
//   - store.Store: Persistence seam.
func (s *Service) Store() store.Store {
	return s.store
}

// Logs returns the log backend.
//
// Returns:
//   - stream.Logs: Stream seam.
func (s *Service) Logs() stream.Logs {
	return s.logs
}

// SetPool publishes live worker counts for GET /v1/host.
//
// Parameters:
//   - size: Started DinD count.
//   - busy: Workers currently in a case.
//   - idle: Workers waiting for work.
func (s *Service) SetPool(size, busy, idle int) {
	s.mu.Lock()
	s.poolSize = size
	s.busy = busy
	s.idle = idle
	s.mu.Unlock()
}

// Pool returns live worker counts.
//
// Returns:
//   - int: Started DinD count.
//   - int: Workers currently in a case.
//   - int: Workers waiting for work.
func (s *Service) Pool() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.poolSize, s.busy, s.idle
}

// SetCurrentCases records which cases are in-flight on the active run.
//
// Parameters:
//   - runID: Sitting that owns the workers.
//   - ids: Case IDs currently executing.
func (s *Service) SetCurrentCases(runID string, ids []string) {
	s.mu.Lock()
	if s.currentID == runID {
		s.currentIDs = slices.Clone(ids)
	}
	s.mu.Unlock()
}

// AddCurrentCase records one in-flight case without dropping the others.
//
// Parameters:
//   - runID: Sitting that owns the workers.
//   - caseID: Case that just started.
func (s *Service) AddCurrentCase(runID, caseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentID != runID || caseID == "" {
		return
	}

	if slices.Contains(s.currentIDs, caseID) {
		return
	}

	s.currentIDs = append(s.currentIDs, caseID)
}

// RemoveCurrentCase drops one in-flight case.
//
// Parameters:
//   - runID: Sitting that owns the workers.
//   - caseID: Case that just finished.
func (s *Service) RemoveCurrentCase(runID, caseID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentID != runID {
		return
	}

	s.currentIDs = slices.DeleteFunc(s.currentIDs, func(id string) bool { return id == caseID })
}

// ListCases proxies the store.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting UUID.
//   - filter: Status, query, pagination.
//
// Returns:
//   - []store.Case: Page of cases.
//   - int: Total matches.
//   - error: Store failure.
func (s *Service) ListCases(ctx context.Context, runID string, filter store.CaseListFilter) ([]store.Case, int, error) {
	return s.store.ListCases(ctx, runID, filter)
}

// GetCase proxies the store.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting UUID.
//   - caseID: Case identifier.
//
// Returns:
//   - store.Case: Row.
//   - error: Store failure.
func (s *Service) GetCase(ctx context.Context, runID, caseID string) (store.Case, error) {
	return s.store.GetCase(ctx, runID, caseID)
}

// QueryLogs proxies the log backend.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting UUID.
//   - caseID: Case identifier.
//   - streamName: stdout, stderr, or empty for both.
//
// Returns:
//   - []stream.Line: Log lines.
//   - error: Backend failure.
func (s *Service) QueryLogs(ctx context.Context, runID, caseID, streamName string) ([]stream.Line, error) {
	return s.logs.Query(ctx, runID, caseID, streamName)
}

// LogsReady reports whether the log backend can accept writes.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Backend failure.
func (s *Service) LogsReady(ctx context.Context) error {
	return s.logs.Ready(ctx)
}

// ListEvents proxies the store event log.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting UUID.
//   - afterID: Exclusive cursor.
//   - limit: Page size.
//
// Returns:
//   - []store.Event: Events oldest first.
//   - error: Store failure.
func (s *Service) ListEvents(ctx context.Context, runID string, afterID int64, limit int) ([]store.Event, error) {
	return s.store.ListEvents(ctx, runID, afterID, limit)
}

// StorePing lists one sitting to prove the store is reachable.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Store failure.
func (s *Service) StorePing(ctx context.Context) error {
	_, err := s.store.ListRuns(ctx, store.RunListFilter{Limit: 1})

	return err
}

// setLoopErr records the last dispatcher transport error.
//
// Parameters:
//   - err: Error to store. Nil clears it.
func (s *Service) setLoopErr(err error) {
	s.mu.Lock()
	s.loopErr = err
	s.mu.Unlock()
}

// LoopErr is the last non-ErrNotFound dispatcher error.
//
// Returns:
//   - error: Last pump transport error, or nil.
func (s *Service) LoopErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loopErr
}

// HostSnapshot fills discovery plus live pool.
//
// Parameters:
//   - diskPath: Filesystem to measure.
//
// Returns:
//   - host.Snapshot: Capacity and pool.
//   - error: Discovery failure.
func (s *Service) HostSnapshot(diskPath string) (host.Snapshot, error) {
	snap, err := host.Discover(diskPath)
	if err != nil {
		return host.Snapshot{}, err
	}

	size, busy, idle := s.Pool()
	snap.PoolSize = size
	snap.BusyWorkers = busy
	snap.IdleWorkers = idle

	return snap, nil
}

// clearCurrent drops the execution-slot identity when it matches id.
//
// Parameters:
//   - id: Sitting that just left the slot.
func (s *Service) clearCurrent(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentID == id {
		s.currentID = ""
		s.currentIDs = nil
		s.cancel = nil
	}
}
