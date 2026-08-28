package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

const (
	// permDir is the mode for case artifact directories.
	permDir = 0o750
	// permFile is the mode for meta.json and failure.txt.
	permFile = 0o600
)

// Meta is per-case metadata written to meta.json.
type Meta struct {
	ID      string            `json:"id"`
	Argv    []string          `json:"argv"`
	Env     map[string]string `json:"env"`
	Factors map[string]string `json:"factors"`
	Expect  engine.Expect     `json:"expect"`
	Passed  bool              `json:"passed"`
	Error   string            `json:"error,omitempty"`
}

// WriteCaseDir creates artifacts/<run>/cases/<id>/ and writes meta.json.
//
// Parameters:
//   - runDir: artifacts/<run-id>.
//   - meta: Case metadata.
//
// Returns:
//   - string: Case directory path.
//   - error: Write error.
func WriteCaseDir(runDir string, meta Meta) (string, error) {
	dir := filepath.Join(runDir, "cases", meta.ID)

	mkdirErr := os.MkdirAll(dir, permDir)
	if mkdirErr != nil {
		return "", fmt.Errorf("case dir: %w", mkdirErr)
	}

	raw, marshalErr := json.MarshalIndent(meta, "", "  ")
	if marshalErr != nil {
		return "", fmt.Errorf("marshal meta: %w", marshalErr)
	}

	writeErr := os.WriteFile(filepath.Join(dir, "meta.json"), raw, permFile)
	if writeErr != nil {
		return "", fmt.Errorf("write meta.json: %w", writeErr)
	}

	return dir, nil
}

// WriteFailure writes failure.txt for a failed case.
//
// Parameters:
//   - caseDir: Case artifact directory.
//   - message: Failure text.
//
// Returns:
//   - error: Write error.
func WriteFailure(caseDir, message string) error {
	err := os.WriteFile(filepath.Join(caseDir, "failure.txt"), []byte(message), permFile)
	if err != nil {
		return fmt.Errorf("write failure.txt: %w", err)
	}

	return nil
}
