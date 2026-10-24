package containerfixture

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dockerContainer "github.com/moby/moby/api/types/container"
)

// TestInspect_UpdatesDoNotShareStructs verifies that one Update applied to two
// Inspect calls gives each call its own state and health check structs, so a
// change to one container's data does not show up in the other's.
func TestInspect_UpdatesDoNotShareStructs(t *testing.T) {
	t.Parallel()

	state := WithContainerState(dockerContainer.State{Running: false, Status: "exited"})
	health := WithHealthcheck(dockerContainer.HealthConfig{Test: []string{"CMD", "true"}})
	imageHealth := WithImageHealthcheck(dockerContainer.HealthConfig{Test: []string{"CMD", "true"}})

	firstContainer, firstImage := Inspect(state, health, imageHealth)
	secondContainer, secondImage := Inspect(state, health, imageHealth)

	assert.NotSame(t, firstContainer.State, secondContainer.State)
	assert.NotSame(t, firstContainer.Config.Healthcheck, secondContainer.Config.Healthcheck)
	assert.NotSame(t, firstImage.Config.Healthcheck, secondImage.Config.Healthcheck)

	firstContainer.State.Status = "running"

	assert.Equal(t, dockerContainer.ContainerState("exited"), secondContainer.State.Status)
}
