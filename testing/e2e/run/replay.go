package run

import (
	"fmt"
	"path/filepath"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// Replay validates a stored case directory. Meta.json is not YAML, so callers
// re-run named regressions with generator file.
//
// Parameters:
//   - caseDir: artifacts/<run>/cases/<id>.
//
// Returns:
//   - error: ErrReplayNeedsCase, ErrReplayUseFile, or YAML parse wrap.
func Replay(caseDir string) error {
	if caseDir == "" {
		return ErrReplayNeedsCase
	}

	_, err := engine.LoadFile(filepath.Join(caseDir, "meta.json"))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrReplayUseFile, err)
	}

	return ErrReplayUseFile
}
