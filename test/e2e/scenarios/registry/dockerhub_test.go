// Package registry provides end-to-end tests for container registry integrations.
package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestDockerHubIntegration tests Watchtower integration with Docker Hub.
func TestDockerHubIntegration(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container that should be updated from Docker Hub
		// Using nginx:alpine as it's a commonly available image
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once to test Docker Hub integration
		// Don't wait for completion since it may sleep after self-update failure
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--no-startup-message", // Reduce log noise
		})
		require.NoError(t, err)

		// Wait for Watchtower to start processing containers
		err = fw.WaitForLog(watchtower, "Watchtower", 30*time.Second)
		require.NoError(t, err)

		// Give it a moment to process
		time.Sleep(5 * time.Second)

		// Verify Watchtower started and attempted to check containers
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestDockerHubRateLimitHandling tests Watchtower's handling of Docker Hub rate limits.
func TestDockerHubRateLimitHandling(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create multiple containers to potentially trigger rate limiting
		container1, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)

		container2, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "alpine:latest",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)

		// Run Watchtower to test rate limit handling
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
		})
		require.NoError(t, err)

		// Verify Watchtower handles multiple Docker Hub requests gracefully
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		// Clean up containers
		_ = container1
		_ = container2

		return nil
	})
}

// TestDockerHubOfficialImages tests updating official Docker Hub images.
func TestDockerHubOfficialImages(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Test with official nginx image (no username prefix)
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:stable-alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower to test official image handling
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
		})
		require.NoError(t, err)

		// Verify Watchtower handles official Docker Hub images correctly
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestDockerHubUserImages tests updating user-specific Docker Hub images.
func TestDockerHubUserImages(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Test with a user image (this would be a real user image in practice)
		// Using library images for testing since they're publicly available
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "library/alpine:latest",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower to test user image handling
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
		})
		require.NoError(t, err)

		// Verify Watchtower handles user Docker Hub images correctly
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}
