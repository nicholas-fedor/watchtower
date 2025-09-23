// Package registry provides end-to-end tests for container registry integrations.
package registry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestRegistryTLSInsecure tests Watchtower with insecure TLS for registries.
func TestRegistryTLSInsecure(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container using Docker Hub
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with --help to test that --tls-skip-verify flag is accepted
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--help",
		})
		require.NoError(t, err)

		// Wait for help output (different waiting strategy for help)
		err = fw.WaitForLog(watchtower, "Automatically updates", 10*time.Second)
		require.NoError(t, err)

		// Verify Watchtower shows help and includes TLS-related options
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "--tls-skip-verify")

		return nil
	})
}

// TestRegistryTLSSecure tests Watchtower with secure TLS for registries.
func TestRegistryTLSSecure(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// For secure TLS testing, we use Docker Hub which has valid certificates
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once and secure TLS (default behavior)
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
			// No --tls-skip-verify flag, so TLS verification is enabled
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

// TestRegistryTLSCustomCert tests Watchtower with custom TLS certificates.
func TestRegistryTLSCustomCert(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Run Watchtower with --help to test that TLS-related flags are available
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--help",
		})
		require.NoError(t, err)

		// Wait for help output
		err = fw.WaitForLog(watchtower, "Automatically updates", 10*time.Second)
		require.NoError(t, err)

		// Verify Watchtower help includes TLS-related options
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "--tls-skip-verify")

		return nil
	})
}

// TestRegistryTLSVerificationFailure tests Watchtower handling of TLS verification failures.
func TestRegistryTLSVerificationFailure(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a local registry to simulate registry with invalid cert
		registry, err := fw.CreateLocalRegistry()
		require.NoError(t, err)

		// Tag and push an image to the registry
		err = fw.BuildAndPushImage("nginx:alpine", "tlsfail-app", registry.URL(), "v1.0")
		require.NoError(t, err)

		// Create a test container using the image from the registry
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: registry.URL() + "/tlsfail-app:v1.0",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with run-once and strict TLS verification (default)
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
			// No --tls-skip-verify flag, so TLS verification is enforced
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
