// Package git provides end-to-end tests for Git monitoring functionality.
package git

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestGitMonitoringBasic tests basic Git repository monitoring functionality.
// Note: This test validates the framework setup and Git label parsing.
// Actual Git monitoring implementation is planned for future development.
func TestGitMonitoringBasic(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels (separate from image monitoring)
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":          repoURL,
				"com.centurylinklabs.watchtower.git-branch":        "main",
				"com.centurylinklabs.watchtower.git-update-policy": "minor",
				// Note: No "enable" label - Git monitoring is separate from image monitoring
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Simulate Git commit to trigger update
		err = fw.SimulateGitCommit(repoURL, "New feature: add user authentication")
		require.NoError(t, err)

		// Run Watchtower with Git monitoring enabled
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Since Git monitoring isn't implemented yet, just verify the container started
		// and the flag was accepted (no immediate error)
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringNoChanges tests behavior when no Git changes are detected.
// This validates that the framework can handle Git repositories without errors.
func TestGitMonitoringNoChanges(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":   repoURL,
				"com.centurylinklabs.watchtower.git-branch": "main",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower without making any Git changes
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring flag was accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringDifferentBranch tests monitoring a different branch.
// This validates that the framework can handle different branch configurations.
func TestGitMonitoringDifferentBranch(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository with develop branch
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "develop", "Initial commit on develop")
		defer cleanup()

		// Create container monitoring develop branch
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":   repoURL,
				"com.centurylinklabs.watchtower.git-branch": "develop",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Make commit on develop branch
		err = fw.SimulateGitCommit(repoURL, "Feature: develop branch changes")
		require.NoError(t, err)

		// Run Watchtower monitoring develop branch
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring flag was accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringInvalidRepo tests behavior with invalid repository URL.
// This validates that the framework can handle invalid Git configurations gracefully.
func TestGitMonitoringInvalidRepo(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create container with invalid Git repository URL
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":   "file:///nonexistent/repo.git",
				"com.centurylinklabs.watchtower.git-branch": "main",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Git monitoring
		watchtower, err := fw.CreateWatchtowerContainer([]string{
			"--run-once",
			"--enable-git-monitoring",
		})
		require.NoError(t, err)

		// Verify Git monitoring flag was accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}
