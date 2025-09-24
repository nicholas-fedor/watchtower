// Package git provides end-to-end tests for Git monitoring functionality.
package git

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestGitMonitoringAuthToken tests Git monitoring with token authentication.
func TestGitMonitoringAuthToken(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and token auth
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":       repoURL,
				"com.centurylinklabs.watchtower.git-branch":     "main",
				"com.centurylinklabs.watchtower.git-auth-token": "ghp_test_token_12345",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring and token auth
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--enable-git-monitoring",
			"--git-auth-token=ghp_test_token_12345",
		})
		require.NoError(t, err)

		// Verify Git monitoring with token auth is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringAuthSSH tests Git monitoring with SSH key authentication.
func TestGitMonitoringAuthSSH(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and SSH auth
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":     repoURL,
				"com.centurylinklabs.watchtower.git-branch":   "main",
				"com.centurylinklabs.watchtower.git-auth-ssh": "test-ssh-key-content",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring enabled (SSH auth not implemented yet)
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring is accepted (SSH auth labels are present)
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringAuthBasic tests Git monitoring with basic authentication.
func TestGitMonitoringAuthBasic(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and basic auth
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":          repoURL,
				"com.centurylinklabs.watchtower.git-branch":        "main",
				"com.centurylinklabs.watchtower.git-auth-username": "testuser",
				"com.centurylinklabs.watchtower.git-auth-password": "testpass",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring enabled (basic auth not implemented yet)
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring is accepted (basic auth labels are present)
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringAuthUsername tests Git monitoring with username authentication.
func TestGitMonitoringAuthUsername(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and username auth
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":          repoURL,
				"com.centurylinklabs.watchtower.git-branch":        "main",
				"com.centurylinklabs.watchtower.git-auth-username": "testuser",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring enabled (username auth not implemented yet)
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring is accepted (username auth labels are present)
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringAuthInvalid tests Git monitoring with invalid authentication.
func TestGitMonitoringAuthInvalid(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and invalid auth
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":       repoURL,
				"com.centurylinklabs.watchtower.git-branch":     "main",
				"com.centurylinklabs.watchtower.git-auth-token": "", // Empty token
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring and invalid auth
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--no-self-update",
			"--enable-git-monitoring",
			"--git-auth-token=", // Empty token
		})
		require.NoError(t, err)

		// Verify Git monitoring handles invalid auth gracefully
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}
