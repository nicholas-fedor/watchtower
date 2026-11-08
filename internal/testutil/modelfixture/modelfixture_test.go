package modelfixture

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
)

// TestContainer verifies that the container carries the fixture's default
// inspect data with each update applied.
func TestContainer(t *testing.T) {
	t.Parallel()

	c := Container(containerfixture.WithName("app"), containerfixture.WithImageName("app:latest"))

	assert.Equal(t, "app", c.Name())
	assert.Equal(t, "app:latest", c.ImageName())
	assert.True(t, c.IsRunning())
}

// TestContainerWithoutImageInfo verifies that the container keeps its own
// inspect data and reports no image info or image ID.
func TestContainerWithoutImageInfo(t *testing.T) {
	t.Parallel()

	c := ContainerWithoutImageInfo(containerfixture.WithName("app"), containerfixture.WithImageName("app:latest"))

	assert.Equal(t, "app", c.Name())
	assert.Equal(t, "app:latest", c.ImageName())
	assert.Nil(t, c.ImageInfo())
	assert.Empty(t, c.ImageID())
}
