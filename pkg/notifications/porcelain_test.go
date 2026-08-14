package notifications

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
