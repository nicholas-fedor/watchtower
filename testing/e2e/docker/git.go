package docker

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

const dirtySHA = "dirty"

// GitSHA returns a short git revision for run IDs, or "dirty" when git fails.
//
// Parameters:
//   - ctx: Cancellation.
//   - moduleRoot: testing/e2e directory.
//
// Returns:
//   - string: Short SHA or dirty.
func GitSHA(ctx context.Context, moduleRoot string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", filepath.Join(moduleRoot, "..", ".."), "rev-parse", "--short", "HEAD") //nolint:gosec // G204: local git describe for run-id only.

	raw, err := cmd.Output()
	if err != nil {
		return dirtySHA
	}

	return strings.TrimSpace(string(raw))
}
