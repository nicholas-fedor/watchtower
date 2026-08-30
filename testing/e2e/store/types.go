package store

import (
	"encoding/json"
	"time"
)

// RunStatus is the lifecycle of one sitting.
type RunStatus string

const (
	// RunQueued is waiting for the single execution slot.
	RunQueued RunStatus = "queued"
	// RunRunning has workers executing cases.
	RunRunning RunStatus = "running"
	// RunInterrupted stopped with work remaining (crash or cancel).
	RunInterrupted RunStatus = "interrupted"
	// RunCompleted finished the selected stream. Case pass/fail is in the counts.
	RunCompleted RunStatus = "completed"
	// RunFailed means the sitting itself did not finish (setup or scheduler error).
	RunFailed RunStatus = "failed"
	// RunCanceled was stopped by the operator before completion.
	RunCanceled RunStatus = "canceled"
)

// CaseStatus is the lifecycle of one case inside a run.
type CaseStatus string

const (
	// CasePending is selected but not started.
	CasePending CaseStatus = "pending"
	// CaseRunning is executing on a worker.
	CaseRunning CaseStatus = "running"
	// CasePass met Expect and invariants.
	CasePass CaseStatus = "pass"
	// CaseFail did not meet Expect.
	CaseFail CaseStatus = "fail"
	// CaseSkip was filtered, resumed, or unrealizable.
	CaseSkip CaseStatus = "skip"
	// CaseInterrupted was in-flight when the process died.
	CaseInterrupted CaseStatus = "interrupted"
)

// EventKind is an SSE/event-log entry type.
type EventKind string

const (
	// EventRunStatus is a run-level status change.
	EventRunStatus EventKind = "run_status"
	// EventCaseStart is a case beginning on a worker.
	EventCaseStart EventKind = "case_start"
	// EventCaseEnd is a case finishing (pass/fail/skip/interrupted).
	EventCaseEnd EventKind = "case_end"
	// EventCounts is an updated pass/fail/skip tally.
	EventCounts EventKind = "counts"
	// EventPool is a worker pool size or busy/idle change.
	EventPool EventKind = "pool"
)

// Spec is the operator input that created a run.
type Spec struct {
	// Generator is product, random, or file.
	Generator string `json:"generator"`
	// Seed is the random generator seed.
	Seed int64 `json:"seed,omitzero"`
	// Topic is a named development slice.
	Topic string `json:"topic,omitempty"`
	// Filter is a regex on case ID or factor values.
	Filter string `json:"filter,omitempty"`
	// FilePath is the YAML path when Generator is file.
	FilePath string `json:"file,omitempty"`
	// Shard is i/n, or empty for all shards.
	Shard string `json:"shard,omitempty"`
	// Offset skips this many selected cases.
	Offset int `json:"offset,omitzero"`
	// Limit stops after N executed cases. Zero means no cap.
	Limit int `json:"limit,omitzero"`
	// Workers is the requested DinD count. Zero means host auto-detect.
	Workers int `json:"workers,omitzero"`
	// Keep retains extra documents for passing cases (always stored as rows).
	Keep bool `json:"keep,omitzero"`
}

// Run is one sitting.
type Run struct {
	// ID is a UUID v7.
	ID string `json:"id"`
	// Label is a human stamp (time + git sha).
	Label string `json:"label"`
	// CreatedAt is when the run was accepted.
	CreatedAt time.Time `json:"created_at"`
	// StartedAt is when workers began. Zero if still queued.
	StartedAt time.Time `json:"started_at,omitzero"`
	// FinishedAt is when the sitting left the running state.
	FinishedAt time.Time `json:"finished_at,omitzero"`
	// Status is the lifecycle value.
	Status RunStatus `json:"status"`
	// Spec is the creating request.
	Spec Spec `json:"spec"`
	// Passed is the pass count.
	Passed int `json:"passed"`
	// Failed is the fail count.
	Failed int `json:"failed"`
	// Skipped is the skip count.
	Skipped int `json:"skipped"`
	// Error is a sitting-level failure message.
	Error string `json:"error,omitempty"`
	// CurrentIDs are case IDs currently running. Live only and not persisted.
	CurrentIDs []string `json:"current_ids,omitempty"`
}

// Case is one executed or in-flight vector.
type Case struct {
	// RunID is the parent sitting.
	RunID string `json:"run_id"`
	// CaseID is the stable case identifier.
	CaseID string `json:"case_id"`
	// Status is pass, fail, skip, running, pending, or interrupted.
	Status CaseStatus `json:"status"`
	// Factors is the cartesian assignment.
	Factors map[string]string `json:"factors,omitempty"`
	// Expect is the derived expectation document.
	Expect json.RawMessage `json:"expect,omitempty"`
	// Argv is the Watchtower argument vector.
	Argv []string `json:"argv,omitempty"`
	// Env is the Watchtower extra environment.
	Env map[string]string `json:"env,omitempty"`
	// Error is a failure message.
	Error string `json:"error,omitempty"`
	// DurationMs is wall time for the case.
	DurationMs int64 `json:"duration_ms,omitzero"`
	// InspectBefore is the pre-session subject inspect snapshot.
	InspectBefore json.RawMessage `json:"inspect_before,omitempty"`
	// InspectAfter is the post-session subject inspect snapshot.
	InspectAfter json.RawMessage `json:"inspect_after,omitempty"`
	// Porcelain is parsed porcelain JSON when requested.
	Porcelain json.RawMessage `json:"porcelain,omitempty"`
	// HTTPDetails is the containers/details body when probed.
	HTTPDetails string `json:"http_details,omitempty"`
	// StartedAt is when the worker acquired the case.
	StartedAt time.Time `json:"started_at,omitzero"`
	// FinishedAt is when the case reached a terminal status.
	FinishedAt time.Time `json:"finished_at,omitzero"`
}

// Event is one append-only control-plane event.
type Event struct {
	// ID is the monotonic event id (Postgres bigserial / memory counter).
	ID int64 `json:"id"`
	// RunID is the parent sitting.
	RunID string `json:"run_id"`
	// CaseID is set for case-scoped events.
	CaseID string `json:"case_id,omitempty"`
	// Kind is the event type.
	Kind EventKind `json:"kind"`
	// Payload is a small JSON document.
	Payload json.RawMessage `json:"payload,omitempty"`
	// CreatedAt is when the event was recorded.
	CreatedAt time.Time `json:"created_at"`
}

// CaseListFilter selects rows for GET /v1/runs/{id}/cases.
type CaseListFilter struct {
	// Status restricts to one case status. Empty means all.
	Status CaseStatus
	// Query matches case ID or factor name/value (substring).
	Query string
	// Limit is the page size. Values below 1 become 50. Capped at 200.
	Limit int
	// Offset skips this many matching rows.
	Offset int
}

// RunListFilter selects GET /v1/runs.
type RunListFilter struct {
	// Status restricts to one run status. Empty means all.
	Status RunStatus
	// Limit is the page size. Values below 1 become 50. Capped at 200.
	Limit int
	// Offset skips this many matching rows.
	Offset int
}
