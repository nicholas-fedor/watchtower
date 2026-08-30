package store

import "sync"

// Memory is an in-process Store for unit tests and API tests without Docker.
type Memory struct {
	mu sync.Mutex
	// runs is sittings keyed by UUID.
	runs map[string]Run
	// cases is cases keyed by run UUID then case ID.
	cases map[string]map[string]Case
	// events is the append-only event log.
	events []Event
	// nextEv is the last assigned event ID.
	nextEv int64
}

// NewMemory returns an empty memory store.
//
// Returns:
//   - *Memory: Ready store.
func NewMemory() *Memory {
	return &Memory{
		runs:  make(map[string]Run),
		cases: make(map[string]map[string]Case),
	}
}

// Close is a no-op for memory.
//
// Returns:
//   - error: Always nil.
func (m *Memory) Close() error {
	return nil
}
