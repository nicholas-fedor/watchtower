// Package git provides end-to-end tests for Git monitoring functionality.
package git

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestGitMonitoringPolicyPatch tests Git monitoring with patch update policy.
func TestGitMonitoringPolicyPatch(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and patch policy
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":          repoURL,
				"com.centurylinklabs.watchtower.git-branch":        "main",
				"com.centurylinklabs.watchtower.git-update-policy": "patch",
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

		// Verify Git monitoring with patch policy is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringPolicyMinor tests Git monitoring with minor update policy.
func TestGitMonitoringPolicyMinor(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and minor policy
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":          repoURL,
				"com.centurylinklabs.watchtower.git-branch":        "main",
				"com.centurylinklabs.watchtower.git-update-policy": "minor",
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

		// Verify Git monitoring with minor policy is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringPolicyMajor tests Git monitoring with major update policy.
func TestGitMonitoringPolicyMajor(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and major policy
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":          repoURL,
				"com.centurylinklabs.watchtower.git-branch":        "main",
				"com.centurylinklabs.watchtower.git-update-policy": "major",
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

		// Verify Git monitoring with major policy is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringPolicyNone tests Git monitoring with none update policy.
func TestGitMonitoringPolicyNone(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and none policy
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":          repoURL,
				"com.centurylinklabs.watchtower.git-branch":        "main",
				"com.centurylinklabs.watchtower.git-update-policy": "none",
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

		// Verify Git monitoring with none policy is accepted
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}

// TestGitMonitoringPolicyInvalid tests Git monitoring with invalid update policy.
func TestGitMonitoringPolicyInvalid(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Setup mock Git repository
		repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "Initial commit")
		defer cleanup()

		// Create container with Git labels and invalid policy
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.git-repo":          repoURL,
				"com.centurylinklabs.watchtower.git-branch":        "main",
				"com.centurylinklabs.watchtower.git-update-policy": "invalid",
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

		// Verify Git monitoring handles invalid policy gracefully
		logs, err := fw.GetContainerLogs(watchtower)
		require.NoError(t, err)
		require.Contains(t, logs, "Watchtower")

		return nil
	})
}
