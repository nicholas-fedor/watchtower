// Package registry provides end-to-end tests for container registry integrations.
package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestHarborIntegration tests Watchtower integration with Harbor registry.
func TestHarborIntegration(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a local registry to simulate Harbor
		registry, err := fw.CreateLocalRegistry()
		require.NoError(t, err)

		// Tag and push an existing image to the local registry
		err = fw.BuildAndPushImage("nginx:alpine", "test-app", registry.URL(), "v1.0")
		require.NoError(t, err)

		// Create a test container using the image from the local registry
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: registry.URL() + "/test-app:v1.0",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Update the image to simulate a new version
		err = fw.UpdateTestImage(registry.URL()+"/test-app", "v1.0", "v2.0")
		require.NoError(t, err)

		// Run Watchtower with run-once to test Harbor integration
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start processing containers
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Give it a moment to process
		time.Sleep(5 * time.Second)

		// Verify Watchtower started and attempted to check containers
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}

// TestHarborAuthentication tests Watchtower with Harbor authentication.
func TestHarborAuthentication(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a local registry to simulate Harbor
		registry, err := fw.CreateLocalRegistry()
		require.NoError(t, err)

		// Tag and push an existing image to the local registry
		err = fw.BuildAndPushImage("nginx:alpine", "test-app", registry.URL(), "v1.0")
		require.NoError(t, err)

		// Create a test container with Harbor authentication labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: registry.URL() + "/test-app:v1.0",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":    "true",
				"com.centurylinklabs.watchtower.repo-user": "harbor-user",
				"com.centurylinklabs.watchtower.repo-pass": "harbor-token",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once to test Harbor authentication
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start processing containers
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Give it a moment to process
		time.Sleep(5 * time.Second)

		// Verify Watchtower started and attempted to check containers
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}

// TestHarborProjects tests Watchtower with Harbor project structure.
func TestHarborProjects(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a local registry to simulate Harbor
		registry, err := fw.CreateLocalRegistry()
		require.NoError(t, err)

		// Tag and push an existing image to a project-like structure
		err = fw.BuildAndPushImage("nginx:alpine", "myproject/myapp", registry.URL(), "v1.0")
		require.NoError(t, err)

		// Create a test container using the project-structured image
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: registry.URL() + "/myproject/myapp:v1.0",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Update the image to simulate a new version
		err = fw.UpdateTestImage(registry.URL()+"/myproject/myapp", "v1.0", "v2.0")
		require.NoError(t, err)

		// Run Watchtower with run-once to test Harbor project structure
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start processing containers
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Give it a moment to process
		time.Sleep(5 * time.Second)

		// Verify Watchtower started and attempted to check containers
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}
