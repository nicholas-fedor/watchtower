package report

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

func TestWriteSummaryAndJUnit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	summary := Summary{
		RunID:    "testrun",
		Started:  time.Now().Add(-time.Minute),
		Finished: time.Now(),
		Passed:   1,
		Failed:   1,
		Cases: []engine.Result{
			{CaseID: "ok", Passed: true, Status: "pass"},
			{CaseID: "bad", Passed: false, Status: "fail", Err: "boom"},
		},
	}

	require.NoError(t, Write(dir, summary))
	require.FileExists(t, filepath.Join(dir, "summary.json"))
	require.FileExists(t, filepath.Join(dir, "summary.md"))
	require.FileExists(t, filepath.Join(dir, "junit.xml"))

	raw, err := os.ReadFile(filepath.Join(dir, "junit.xml"))
	require.NoError(t, err)
	require.Contains(t, string(raw), "boom")
	require.Contains(t, string(raw), "testcase")
}
