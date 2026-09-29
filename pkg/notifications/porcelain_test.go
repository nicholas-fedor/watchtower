package notifications

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestToPorcelainReport_NilInput(t *testing.T) {
	t.Parallel()

	report := ToPorcelainReport(nil)

	assert.Empty(t, report.Containers)
	assert.Empty(t, report.Containers)
}

func TestToPorcelainJSON_NilInput(t *testing.T) {
	t.Parallel()

	result := ToPorcelainJSON(nil)

	require.NotEmpty(t, result)
	assert.JSONEq(t, `{
  "containers": []
}`, result)
}

func TestToPorcelainJSON_GitAndOCIFields(t *testing.T) {
	t.Parallel()

	status := session.NewContainerStatus("app", "org/app:latest")
	status.SetGitMetadata(
		"https://github.com/org/app.git",
		"main",
		"https://github.com/org/app/releases",
		"https://github.com/org/app",
		"",
		"",
		"deadbeef",
	)

	jsonOut := ToPorcelainJSON(&session.SingleContainerReport{
		UpdatedReports: []types.ContainerReport{status},
	})
	assert.Contains(t, jsonOut, `"git_repo": "https://github.com/org/app.git"`)
	assert.Contains(t, jsonOut, `"changelog": "https://github.com/org/app/releases"`)
	assert.Contains(t, jsonOut, `"oci_source": "https://github.com/org/app"`)
	assert.Contains(t, jsonOut, `"revision": "deadbeef"`)
}
