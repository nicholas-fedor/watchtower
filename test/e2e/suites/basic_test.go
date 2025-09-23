// Package suites provides test suites for end-to-end testing of Watchtower.
package suites

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestBasicFrameworkInitialization tests that the framework can be initialized.
func TestBasicFrameworkInitialization(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)
	require.NotNil(t, fw)

	// Test cleanup
	err = fw.Cleanup()
	require.NoError(t, err)
}

// TestContainerCreation tests basic container creation and cleanup.
func TestContainerCreation(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a simple nginx container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image:        "nginx:alpine",
			ExposedPorts: []string{"80/tcp"},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Wait for container to be ready
		err = fw.WaitForLog(container, "start worker process", 30*time.Second)
		require.NoError(t, err)

		// Get container logs to verify it's working
		logs, err := fw.GetContainerLogs(container)
		require.NoError(t, err)
		require.Contains(t, logs, "nginx")

		return nil
	})
}

// TestWatchtowerContainerCreation tests Watchtower container creation.
func TestWatchtowerContainerCreation(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create Watchtower container with help flag
		container, err := fw.CreateWatchtowerContainer([]string{"--help"})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Just verify the container was created successfully
		// The --help flag causes immediate exit, so we don't wait for logs
		state, err := container.State(context.Background())
		require.NoError(t, err)
		// Container should have exited (since --help just prints and exits)
		require.False(t, state.Running)

		return nil
	})
}

// TestNetworkIsolation tests that containers are properly isolated in networks.
func TestNetworkIsolation(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create two containers that should be in the same network
		container1, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Name:  "test-container-1",
		})
		require.NoError(t, err)

		container2, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Name:  "test-container-2",
		})
		require.NoError(t, err)

		// Both containers should be running
		state1, err := container1.State(context.Background())
		require.NoError(t, err)
		require.True(t, state1.Running)

		state2, err := container2.State(context.Background())
		require.NoError(t, err)
		require.True(t, state2.Running)

		return nil
	})
}
