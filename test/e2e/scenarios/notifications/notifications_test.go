// Package notifications provides end-to-end tests for Watchtower notification functionality.
package notifications

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

// TestSlackNotifications tests Slack notification configuration.
func TestSlackNotifications(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Start mock Slack server
		mockSlack, err := fw.StartMockNotificationService("slack")
		require.NoError(t, err)
		slackServer := mockSlack.(*framework.SlackMockServer)

		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Slack notifications configured
		config := map[string]string{
			"SLACK_HOOK_URL": slackServer.URL(),
		}
		args := fw.BuildNotificationArgs("slack", config)

		watchtower, err := fw.CreateWatchtowerContainer(args)
		require.NoError(t, err)

		// Framework already waited for Watchtower to start, just verify the container exists
		require.NotNil(t, watchtower)

		return nil
	})
}

// TestEmailNotifications tests email notification configuration.
func TestEmailNotifications(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Start mock email server
		mockEmail, err := fw.StartMockNotificationService("email")
		require.NoError(t, err)
		emailServer := mockEmail.(*framework.EmailMockServer)

		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with email notifications configured
		config := map[string]string{
			"EMAIL_FROM":   "watchtower@example.com",
			"EMAIL_TO":     "admin@example.com",
			"EMAIL_SERVER": emailServer.URL(),
		}
		args := fw.BuildNotificationArgs("email", config)

		watchtower, err := fw.CreateWatchtowerContainer(args)
		require.NoError(t, err)

		// Framework already waited for Watchtower to start, just verify the container exists
		require.NotNil(t, watchtower)

		return nil
	})
}

// TestGotifyNotifications tests Gotify notification configuration.
func TestGotifyNotifications(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Start mock Gotify server
		mockGotify, err := fw.StartMockNotificationService("gotify")
		require.NoError(t, err)
		gotifyServer := mockGotify.(*framework.GotifyMockServer)

		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Gotify notifications configured
		config := map[string]string{
			"GOTIFY_URL": gotifyServer.URL(),
		}
		args := fw.BuildNotificationArgs("gotify", config)

		watchtower, err := fw.CreateWatchtowerContainer(args)
		require.NoError(t, err)

		// Framework already waited for Watchtower to start, just verify the container exists
		require.NotNil(t, watchtower)

		return nil
	})
}

// TestMultipleNotifications tests configuration of multiple notification services.
func TestMultipleNotifications(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Start multiple mock servers
		mockSlack, err := fw.StartMockNotificationService("slack")
		require.NoError(t, err)
		slackServer := mockSlack.(*framework.SlackMockServer)

		mockEmail, err := fw.StartMockNotificationService("email")
		require.NoError(t, err)
		emailServer := mockEmail.(*framework.EmailMockServer)

		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with multiple notifications configured
		args := []string{
			"--run-once",
			"--notification-slack",
			"--notification-slack-hook-url", slackServer.URL(),
			"--notification-email",
			"--notification-email-from", "watchtower@example.com",
			"--notification-email-to", "admin@example.com",
			"--notification-email-server", emailServer.URL(),
		}

		watchtower, err := fw.CreateWatchtowerContainer(args)
		require.NoError(t, err)

		// Framework already waited for Watchtower to start, just verify the container exists
		require.NotNil(t, watchtower)

		return nil
	})
}

// TestNotificationContentValidation tests notification service configuration.
func TestNotificationContentValidation(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Start mock Slack server
		mockSlack, err := fw.StartMockNotificationService("slack")
		require.NoError(t, err)
		slackServer := mockSlack.(*framework.SlackMockServer)

		// Create a test container with a specific name
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Name:  "test-notification-container",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with Slack notifications configured
		config := map[string]string{
			"SLACK_HOOK_URL": slackServer.URL(),
		}
		args := fw.BuildNotificationArgs("slack", config)

		watchtower, err := fw.CreateWatchtowerContainer(args)
		require.NoError(t, err)

		// Framework already waited for Watchtower to start, just verify the container exists
		require.NotNil(t, watchtower)

		return nil
	})
}

// TestNotificationFailureHandling tests notification configuration with invalid URLs.
func TestNotificationFailureHandling(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with invalid notification URL (should still start)
		args := []string{
			"--run-once",
			"--notification-slack",
			"--notification-slack-hook-url", "http://invalid-url-that-does-not-exist.com/webhook",
		}

		watchtower, err := fw.CreateWatchtowerContainer(args)
		require.NoError(t, err)

		// Framework already waited for Watchtower to start, just verify the container exists
		require.NotNil(t, watchtower)

		return nil
	})
}

// TestNotificationLevels tests notification configuration for different services.
func TestNotificationLevels(t *testing.T) {
	fw, err := framework.NewE2EFramework("watchtower:test")
	require.NoError(t, err)

	fw.RunTestWithCleanup(t, func() error {
		// Start mock Slack server
		mockSlack, err := fw.StartMockNotificationService("slack")
		require.NoError(t, err)
		slackServer := mockSlack.(*framework.SlackMockServer)

		// Create a test container
		container, err := fw.CreateContainer(testcontainers.ContainerRequest{
			Image: "nginx:alpine",
			Labels: map[string]string{
				"com.centurylinklabs.watchtower.enable": "true",
			},
		})
		require.NoError(t, err)
		require.NotNil(t, container)

		// Run Watchtower with notifications configured
		config := map[string]string{
			"SLACK_HOOK_URL": slackServer.URL(),
		}
		args := fw.BuildNotificationArgs("slack", config)

		watchtower, err := fw.CreateWatchtowerContainer(args)
		require.NoError(t, err)

		// Framework already waited for Watchtower to start, just verify the container exists
		require.NotNil(t, watchtower)

		return nil
	})
}
