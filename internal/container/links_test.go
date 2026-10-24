package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dockerContainer "github.com/moby/moby/api/types/container"
)

// TestContainer_Links_NoConfig verifies that a container without inspect
// config data still reports its host-config dependencies instead of panicking.
func TestContainer_Links_NoConfig(t *testing.T) {
	t.Parallel()

	c := &Container{
		normalizedName: "app",
		containerInfo: &dockerContainer.InspectResponse{
			Name: "/app",
			HostConfig: &dockerContainer.HostConfig{
				NetworkMode: "container:vpn",
			},
		},
	}

	assert.Equal(t, []string{"vpn"}, c.Links(false))
}
