package update

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerNetwork "github.com/moby/moby/api/types/network"

	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
)

// Test_hasSelfDependency verifies that a container whose depends-on label
// names itself is detected.
func Test_hasSelfDependency(t *testing.T) {
	t.Parallel()

	t.Run("should return false when no depends-on label is present", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels:       map[string]string{},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.False(t, result)
	})

	t.Run("should return false when depends-on label is empty", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.False(t, result)
	})

	t.Run("should return false when depends-on contains other containers", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "other-container",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.False(t, result)
	})

	t.Run("should return true when depends-on contains self", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "test-container",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.True(t, result)
	})

	t.Run("should return true when depends-on contains self among multiple dependencies", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "other-container,test-container,another-container",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.True(t, result)
	})

	t.Run("should handle spaces and trimming correctly", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": " other-container , test-container , another-container ",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.True(t, result)
	})

	t.Run("should handle leading slashes in container names", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower.depends-on": "/test-container",
				},
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.True(t, result)
	})

	t.Run("should return false when Config is nil", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			nil,
		) // Config is nil
		result := hasSelfDependency(container)
		assert.False(t, result)
	})

	t.Run("should return false when Labels is nil", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"test-container",
			"/test-container",
			"test:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels:       nil, // Labels is nil
				ExposedPorts: dockerNetwork.PortSet{},
			},
		)
		result := hasSelfDependency(container)
		assert.False(t, result)
	})
}
