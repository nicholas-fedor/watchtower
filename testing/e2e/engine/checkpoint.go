package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Checkpoint records completed case IDs so a sitting can resume.
type Checkpoint struct {
	mu sync.Mutex

	// RunID is the artifacts directory name for this sitting.
	RunID string `json:"run_id"`
	// Completed maps case ID to pass, fail, or skip.
	Completed map[string]string `json:"completed"`
	// Passed is the count of passing cases.
	Passed int `json:"passed"`
	// Failed is the count of failing cases.
	Failed int `json:"failed"`
	// Skipped is the count of filtered or unrealizable cases.
	Skipped int `json:"skipped"`
	// LastID is the most recently recorded case ID.
	LastID string `json:"last_id"`
	// path is the checkpoint JSON file on disk.
	path string
}

// LoadCheckpoint reads a checkpoint JSON file. Missing files start empty.
//
// Parameters:
//   - path: Checkpoint file path.
//
// Returns:
//   - *Checkpoint: Loaded or empty checkpoint.
//   - error: Read or JSON error for an existing file.
func LoadCheckpoint(path string) (*Checkpoint, error) {
	point := &Checkpoint{
		Completed: make(map[string]string),
		path:      path,
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return point, nil
		}

		return nil, fmt.Errorf("read checkpoint: %w", err)
	}

	unmarshalErr := json.Unmarshal(raw, point)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("parse checkpoint: %w", unmarshalErr)
	}

	if point.Completed == nil {
		point.Completed = make(map[string]string)
	}

	point.path = path

	return point, nil
}

// Has reports whether the case ID already completed.
//
// Parameters:
//   - id: Case identifier.
//
// Returns:
//   - bool: True when resume should skip the case.
func (c *Checkpoint) Has(caseID string) bool {
	if c == nil {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	_, exists := c.Completed[caseID]

	return exists
}

// Record stores one case result and flushes the checkpoint file.
//
// Parameters:
//   - id: Case identifier.
//   - status: pass, fail, or skip.
//
// Returns:
//   - error: Flush error.
func (c *Checkpoint) Record(caseID, status string) error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.Completed[caseID]; exists {
		return nil
	}

	c.Completed[caseID] = status
	c.LastID = caseID

	switch status {
	case "pass":
		c.Passed++
	case "fail":
		c.Failed++
	default:
		c.Skipped++
	}

	return c.flushLocked()
}

// flushLocked writes JSON to disk. Caller holds c.mu.
//
// Returns:
//   - error: Write error.
func (c *Checkpoint) flushLocked() error {
	if c.path == "" {
		return nil
	}

	mkdirErr := os.MkdirAll(filepath.Dir(c.path), permDir)
	if mkdirErr != nil {
		return fmt.Errorf("checkpoint dir: %w", mkdirErr)
	}

	raw, marshalErr := json.MarshalIndent(c, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("marshal checkpoint: %w", marshalErr)
	}

	writeErr := os.WriteFile(c.path, raw, permFile)
	if writeErr != nil {
		return fmt.Errorf("write checkpoint: %w", writeErr)
	}

	return nil
}
