package contract

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Lines returns the entries of a generated contract list, in file order.
//
// Blank lines and lines that start with "#" are left out.
// A file that cannot be read fails the test.
//
// Parameters:
//   - tb: Test to fail when the file cannot be read.
//   - path: Path of the generated list.
//
// Returns:
//   - []string: The list's entries, with surrounding whitespace removed.
func Lines(tb testing.TB, path string) []string {
	tb.Helper()

	contents, err := os.ReadFile(path)
	require.NoError(tb, err)

	var entries []string

	for line := range strings.Lines(string(contents)) {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			entries = append(entries, line)
		}
	}

	return entries
}
