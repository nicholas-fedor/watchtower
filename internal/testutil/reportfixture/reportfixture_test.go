package reportfixture

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestReport verifies that each state puts its container in the matching
// report category. Repeated states get distinct containers.
func TestReport(t *testing.T) {
	t.Parallel()

	report := Report(
		state.UpdatedState,
		state.UpdatedState,
		state.FailedState,
		state.FreshState,
		state.SkippedState,
		state.RestartedState,
		state.StaleState,
	)

	assert.Len(t, report.Updated(), 2)
	assert.Len(t, report.Failed(), 1)
	assert.Len(t, report.Fresh(), 1)
	assert.Len(t, report.Skipped(), 1)
	assert.Len(t, report.Restarted(), 1)
	require.Len(t, report.Stale(), 1)
	assert.True(t, report.Stale()[0].IsMonitorOnly())

	ids := map[types.ContainerID]bool{}
	for _, c := range report.All() {
		assert.False(t, ids[c.ID()], "duplicate container ID %s", c.ID())
		ids[c.ID()] = true
	}

	require.Len(t, report.Failed(), 1)
	assert.Equal(t, "accidentally the whole container", report.Failed()[0].Error())
}

// TestContainer verifies the container's name, image and IDs. An index that
// would reuse another state's IDs panics, as does a state no report ends in.
func TestContainer(t *testing.T) {
	t.Parallel()

	c, newImage := Container(0, state.UpdatedState)

	assert.Equal(t, "updt1", c.Name())
	assert.Equal(t, "mock/updt1:latest", c.ImageName())
	assert.Equal(t, "c7911", string(c.ID())[:5])
	assert.Equal(t, "d0a11", string(newImage)[:5])

	assert.Panics(t, func() { Container(maxContainersPerState, state.UpdatedState) })
	assert.Panics(t, func() { Container(0, state.ScannedState) })
}
