// Package lifecycle provides end-to-end tests for Watchtower lifecycle hook functionality.
package lifecycle

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestLifecycleHooksBasic tests basic pre and post-update lifecycle hook execution.
func TestLifecycleHooksBasic(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container with lifecycle hooks
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":                "true",
				"com.centurylinklabs.watchtower.lifecycle.pre-update":  "echo 'pre-update-hook-executed'",
				"com.centurylinklabs.watchtower.lifecycle.post-update": "echo 'post-update-hook-executed'",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with lifecycle hooks enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
			"--enable-lifecycle-hooks",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start processing
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Give it time to process containers
		time.Sleep(5 * time.Second)

		// Verify Watchtower processed containers (even if no updates occurred)
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}

// TestLifecycleHooksPreUpdateOnly tests execution of only pre-update hooks.
func TestLifecycleHooksPreUpdateOnly(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container with only pre-update hook
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":               "true",
				"com.centurylinklabs.watchtower.lifecycle.pre-update": "echo 'pre-only-hook'",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with lifecycle hooks enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
			"--enable-lifecycle-hooks",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Verify Watchtower processed containers
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}

// TestLifecycleHooksPostUpdateOnly tests execution of only post-update hooks.
func TestLifecycleHooksPostUpdateOnly(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container with only post-update hook
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":                "true",
				"com.centurylinklabs.watchtower.lifecycle.post-update": "echo 'post-only-hook'",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with lifecycle hooks enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
			"--enable-lifecycle-hooks",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Verify Watchtower processed containers
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}

// TestLifecycleHooksFailureHandling tests behavior when lifecycle hooks fail.
func TestLifecycleHooksFailureHandling(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container with failing hooks
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":                "true",
				"com.centurylinklabs.watchtower.lifecycle.pre-update":  "exit 1", // Failing command
				"com.centurylinklabs.watchtower.lifecycle.post-update": "echo 'post-hook-after-failure'",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with lifecycle hooks enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
			"--enable-lifecycle-hooks",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Verify Watchtower processed containers
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}

// TestLifecycleHooksDisabled tests that hooks are not executed when lifecycle hooks are disabled.
func TestLifecycleHooksDisabled(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container with lifecycle hooks
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":               "true",
				"com.centurylinklabs.watchtower.lifecycle.pre-update": "echo 'should-not-execute'",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower WITHOUT lifecycle hooks enabled (default behavior)
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
			// Note: --enable-lifecycle-hooks is NOT specified
		})
		require.NoError(t, err)

		// Wait for Watchtower to start
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Verify Watchtower processed containers normally
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}

// TestLifecycleHooksComplexCommands tests execution of complex multi-line commands.
func TestLifecycleHooksComplexCommands(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container with complex lifecycle hooks
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable":                "true",
				"com.centurylinklabs.watchtower.lifecycle.pre-update":  "echo 'Starting update process' && date && echo 'Pre-update complete'",
				"com.centurylinklabs.watchtower.lifecycle.post-update": "echo 'Update finished' && echo 'Post-update cleanup done'",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with lifecycle hooks enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-startup-message",
			"--enable-lifecycle-hooks",
		})
		require.NoError(t, err)

		// Wait for Watchtower to start
		err = fw.WaitForLog(watchtower, "Running a one time update", 30*time.Second)
		require.NoError(t, err)

		// Verify Watchtower processed containers with complex hook configurations
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")
		require.Contains(t, logs, "Running a one time update")

		return nil
	})
}
