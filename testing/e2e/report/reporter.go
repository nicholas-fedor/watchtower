package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// Summary is the sitting-level report.
type Summary struct {
	RunID    string          `json:"run_id"`
	Started  time.Time       `json:"started"`
	Finished time.Time       `json:"finished"`
	Passed   int             `json:"passed"`
	Failed   int             `json:"failed"`
	Skipped  int             `json:"skipped"`
	Cases    []engine.Result `json:"cases"`
}

// Write dumps summary.json, summary.md, and junit.xml under dir.
//
// Parameters:
//   - dir: artifacts/<run-id>.
//   - summary: Aggregated results.
//
// Returns:
//   - error: Write error.
func Write(dir string, summary Summary) error {
	mkdirErr := os.MkdirAll(dir, permDir)
	if mkdirErr != nil {
		return fmt.Errorf("report dir: %w", mkdirErr)
	}

	raw, marshalErr := json.MarshalIndent(summary, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("marshal summary: %w", marshalErr)
	}

	jsonErr := os.WriteFile(filepath.Join(dir, "summary.json"), raw, permFile)
	if jsonErr != nil {
		return fmt.Errorf("write summary.json: %w", jsonErr)
	}

	mdErr := os.WriteFile(filepath.Join(dir, "summary.md"), []byte(markdown(summary)), permFile)
	if mdErr != nil {
		return fmt.Errorf("write summary.md: %w", mdErr)
	}

	junitErr := os.WriteFile(filepath.Join(dir, "junit.xml"), []byte(JUnit(summary)), permFile)
	if junitErr != nil {
		return fmt.Errorf("write junit.xml: %w", junitErr)
	}

	return nil
}

// markdown renders a short Markdown sitting summary.
//
// Parameters:
//   - summary: Aggregated results.
//
// Returns:
//   - string: Markdown body.
func markdown(summary Summary) string {
	return fmt.Sprintf(
		"# Watchtower e2e %s\n\n- passed: %d\n- failed: %d\n- skipped: %d\n- started: %s\n- finished: %s\n",
		summary.RunID,
		summary.Passed,
		summary.Failed,
		summary.Skipped,
		summary.Started.UTC().Format(time.RFC3339),
		summary.Finished.UTC().Format(time.RFC3339),
	)
}
