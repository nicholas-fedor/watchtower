package containerfixture

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dockerContainer "github.com/moby/moby/api/types/container"
)

// TestInspect_UpdatesDoNotShareStructs verifies that one Update applied to two
// Inspect calls gives each call its own state and health check data. The copy
// reaches the health results and Test commands, so a change to one container's
// data does not show up in the other's.
func TestInspect_UpdatesDoNotShareStructs(t *testing.T) {
	t.Parallel()

	state := WithContainerState(dockerContainer.State{
		Running: false,
		Status:  "exited",
		Health: &dockerContainer.Health{
			Status: dockerContainer.Unhealthy,
			Log:    []*dockerContainer.HealthcheckResult{{ExitCode: 1, Output: "failed"}},
		},
	})
	health := WithHealthcheck(dockerContainer.HealthConfig{Test: []string{"CMD", "true"}})
	imageHealth := WithImageHealthcheck(dockerContainer.HealthConfig{Test: []string{"CMD", "true"}})

	firstContainer, firstImage := Inspect(state, health, imageHealth)
	secondContainer, secondImage := Inspect(state, health, imageHealth)

	assert.NotSame(t, firstContainer.State, secondContainer.State)
	assert.NotSame(t, firstContainer.State.Health, secondContainer.State.Health)
	assert.NotSame(t, firstContainer.State.Health.Log[0], secondContainer.State.Health.Log[0])
	assert.NotSame(t, firstContainer.Config.Healthcheck, secondContainer.Config.Healthcheck)
	assert.NotSame(t, firstImage.Config.Healthcheck, secondImage.Config.Healthcheck)

	firstContainer.State.Status = "running"
	firstContainer.State.Health.Log[0].Output = "changed"
	firstContainer.State.Health.Log = append(firstContainer.State.Health.Log, nil)
	firstContainer.Config.Healthcheck.Test[1] = "false"
	firstImage.Config.Healthcheck.Test[1] = "false"

	assert.Equal(t, dockerContainer.ContainerState("exited"), secondContainer.State.Status)
	assert.Equal(t, []*dockerContainer.HealthcheckResult{{ExitCode: 1, Output: "failed"}}, secondContainer.State.Health.Log)
	assert.Equal(t, []string{"CMD", "true"}, secondContainer.Config.Healthcheck.Test)
	assert.Equal(t, []string{"CMD", "true"}, secondImage.Config.Healthcheck.Test)
}

// TestInspect_NilHealthStaysNil verifies that a state without health results
// still has none after Inspect copies it.
func TestInspect_NilHealthStaysNil(t *testing.T) {
	t.Parallel()

	containerInfo, _ := Inspect(WithContainerState(dockerContainer.State{Status: "exited"}))

	assert.Nil(t, containerInfo.State.Health)
}
