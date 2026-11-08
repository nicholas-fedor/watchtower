package contract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLines verifies that Lines keeps a list's entries in order and leaves out
// the generated header, comments and blank lines.
func TestLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "list.txt")
	contents := "# Generated. DO NOT EDIT.\nFresh\n\n  Updated  \n# a comment\nFailed\n"
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	assert.Equal(t, []string{"Fresh", "Updated", "Failed"}, Lines(t, path))
}
