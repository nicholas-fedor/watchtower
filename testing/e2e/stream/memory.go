package stream

import (
	"context"
	"slices"
	"sync"
)

// Memory is an in-process Logs adapter for tests.
type Memory struct {
	mu sync.Mutex
	// data is lines keyed by run/case/stream.
	data map[string][]Line
}

// NewMemory returns an empty log store.
//
// Returns:
//   - *Memory: Ready store.
func NewMemory() *Memory {
	return &Memory{data: make(map[string][]Line)}
}

// logKey builds the in-memory map key for one case stream.
//
// Parameters:
//   - runID: Sitting id.
//   - caseID: Case id.
//   - streamName: stdout or stderr.
//
// Returns:
//   - string: Map key.
func logKey(runID, caseID, streamName string) string {
	return runID + "/" + caseID + "/" + streamName
}

// Push appends lines.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Logs.
//   - runID: Sitting id.
//   - caseID: Case id.
//   - streamName: stdout or stderr.
//   - lines: Lines to store.
//
// Returns:
//   - error: Always nil.
func (m *Memory) Push(_ context.Context, runID, caseID, streamName string, lines []Line) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := logKey(runID, caseID, streamName)
	m.data[key] = append(m.data[key], slices.Clone(lines)...)

	return nil
}

// Query returns stored lines.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Logs.
//   - runID: Sitting id.
//   - caseID: Case id.
//   - streamName: stdout, stderr, or empty for both.
//
// Returns:
//   - []Line: Stored lines.
//   - error: Always nil.
func (m *Memory) Query(_ context.Context, runID, caseID, streamName string) ([]Line, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if streamName != "" {
		out := m.data[logKey(runID, caseID, streamName)]

		return slices.Clone(out), nil
	}

	out := make([]Line, 0)
	out = append(out, m.data[logKey(runID, caseID, StreamStdout)]...)
	out = append(out, m.data[logKey(runID, caseID, StreamStderr)]...)

	return out, nil
}

// Ready always succeeds.
//
// Parameters:
//   - ctx: Unused. Present to satisfy Logs.
//
// Returns:
//   - error: Always nil.
func (m *Memory) Ready(context.Context) error {
	return nil
}

// Close is a no-op.
//
// Returns:
//   - error: Always nil.
func (m *Memory) Close() error {
	return nil
}
