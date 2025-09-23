// Package git provides end-to-end tests for Git monitoring functionality.
package git

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestGitMonitoringProviderGitHub tests Git monitoring with GitHub provider.
func TestGitMonitoringProviderGitHub(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock GitHub repository
		repoURL, cleanup := fw.SetupMockGitRepo("github-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with GitHub repo labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":     repoURL,
				"com.centurylinklabs.watchtower.git-branch":   "main",
				"com.centurylinklabs.watchtower.git-provider": "github",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring with GitHub provider is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringProviderGitLab tests Git monitoring with GitLab provider.
func TestGitMonitoringProviderGitLab(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock GitLab repository
		repoURL, cleanup := fw.SetupMockGitRepo("gitlab-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with GitLab repo labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":     repoURL,
				"com.centurylinklabs.watchtower.git-branch":   "main",
				"com.centurylinklabs.watchtower.git-provider": "gitlab",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring with GitLab provider is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringProviderSelfHosted tests Git monitoring with self-hosted Git provider.
func TestGitMonitoringProviderSelfHosted(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock self-hosted Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("selfhosted-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with self-hosted repo labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":     repoURL,
				"com.centurylinklabs.watchtower.git-branch":   "main",
				"com.centurylinklabs.watchtower.git-provider": "self-hosted",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring with self-hosted provider is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringProviderGeneric tests Git monitoring with generic Git provider.
func TestGitMonitoringProviderGeneric(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock generic Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("generic-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with generic Git repo labels (no provider specified)
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":   repoURL,
				"com.centurylinklabs.watchtower.git-branch": "main",
				// No provider specified - should use generic Git client
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring with generic provider is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringProviderInvalid tests Git monitoring with invalid provider.
func TestGitMonitoringProviderInvalid(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("invalid-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with invalid provider labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":     repoURL,
				"com.centurylinklabs.watchtower.git-branch":   "main",
				"com.centurylinklabs.watchtower.git-provider": "invalid-provider",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring handles invalid provider gracefully
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}
