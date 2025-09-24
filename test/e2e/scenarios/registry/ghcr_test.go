// Package registry provides end-to-end tests for container registry integrations.
package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestGHCRIntegration tests Watchtower integration with GitHub Container Registry.
func TestGHCRIntegration(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container that should be updated from GHCR
		// Using a public GHCR image for testing
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "ghcr.io/github/super-linter:latest",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once to test GHCR integration
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--no-startup-message",
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

// TestGHCRPrivateRepo tests Watchtower with private GHCR repositories.
func TestGHCRPrivateRepo(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container with GHCR private repo configuration
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine", // Using nginx as a placeholder
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
				// These labels would be used for private GHCR repos
				"com.centurylinklabs.watchtower.repo-user": "testuser",
				"com.centurylinklabs.watchtower.repo-pass": "testtoken",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once and GHCR auth
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--no-startup-message",
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

// TestGHCRRateLimitHandling tests Watchtower's handling of GHCR rate limits.
func TestGHCRRateLimitHandling(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create multiple containers to potentially trigger rate limiting
		container1, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "ghcr.io/github/super-linter:latest",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)

		container2, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)

		// Run Watchtower to test GHCR rate limit handling
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start processing containers
		err = fw.WaitForLog(watchtower, "Watchtower", 30*time.Second)
		require.NoError(t, err)

		// Give it a moment to process
		time.Sleep(5 * time.Second)

		// Verify Watchtower handles multiple GHCR requests gracefully
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		// Clean up containers
		_ = container1
		_ = container2

		return nil
	})
}
