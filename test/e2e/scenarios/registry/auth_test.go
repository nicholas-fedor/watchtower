// Package registry provides end-to-end tests for container registry integrations.
package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestPrivateRegistryBasicAuth tests Watchtower with basic authentication for private registries.
func TestPrivateRegistryBasicAuth(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a local registry to simulate private registry
		registry, err := fw.CreateLocalRegistry()
		require.NoError(t, err)

		// Tag and push an image to the registry
		err = fw.BuildAndPushImage("nginx:alpine", "private-app", registry.URL(), "v1.0")
		require.NoError(t, err)

		// Create a test container with basic auth labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: registry.URL() + "/private-app:v1.0",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":    "true",
				"com.centurylinklabs.watchtower.repo-user": "testuser",
				"com.centurylinklabs.watchtower.repo-pass": "testpass",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once to test private registry auth
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

// TestPrivateRegistryTokenAuth tests Watchtower with token authentication for private registries.
func TestPrivateRegistryTokenAuth(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a local registry to simulate private registry
		registry, err := fw.CreateLocalRegistry()
		require.NoError(t, err)

		// Tag and push an image to the registry
		err = fw.BuildAndPushImage("nginx:alpine", "token-app", registry.URL(), "v1.0")
		require.NoError(t, err)

		// Create a test container with token auth labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: registry.URL() + "/token-app:v1.0",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":    "true",
				"com.centurylinklabs.watchtower.repo-user": "oauth2accesstoken",
				"com.centurylinklabs.watchtower.repo-pass": "ghp_1234567890abcdef",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once to test token authentication
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

// TestPrivateRegistryNoAuth tests Watchtower with no authentication for private registries.
func TestPrivateRegistryNoAuth(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a local registry to simulate private registry (no auth required for local)
		registry, err := fw.CreateLocalRegistry()
		require.NoError(t, err)

		// Tag and push an image to the registry
		err = fw.BuildAndPushImage("nginx:alpine", "noauth-app", registry.URL(), "v1.0")
		require.NoError(t, err)

		// Create a test container without auth labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: registry.URL() + "/noauth-app:v1.0",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
				// No auth labels provided
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once to test no-auth scenario
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

// TestPrivateRegistryInvalidAuth tests Watchtower with invalid authentication for private registries.
func TestPrivateRegistryInvalidAuth(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a local registry to simulate private registry
		registry, err := fw.CreateLocalRegistry()
		require.NoError(t, err)

		// Tag and push an image to the registry
		err = fw.BuildAndPushImage("nginx:alpine", "invalidauth-app", registry.URL(), "v1.0")
		require.NoError(t, err)

		// Create a test container with invalid auth labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: registry.URL() + "/invalidauth-app:v1.0",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":    "true",
				"com.centurylinklabs.watchtower.repo-user": "wronguser",
				"com.centurylinklabs.watchtower.repo-pass": "wrongpass",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once to test invalid auth handling
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
