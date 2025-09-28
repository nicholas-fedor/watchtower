// Package notifications provides mechanisms for sending notifications via various services.
// This file implements Microsoft Teams notification functionality.
package notifications

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/teams"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// msTeamsType is the identifier for Microsoft Teams notifications.
const msTeamsType = "msteams"

// Errors for Microsoft Teams notification configuration.
var (
	// errParseWebhookFailed indicates a failure to parse the Microsoft Teams webhook URL.
	errParseWebhookFailed = errors.New("failed to parse Microsoft Teams webhook URL")
	// errConfigWebhookFailed indicates a failure to create a Teams config from the webhook URL.
	errConfigWebhookFailed = errors.New("failed to create Microsoft Teams config from webhook URL")
)

// msTeamsTypeNotifier handles Microsoft Teams notifications via webhook.
//
// It supports optional data inclusion for detailed messages.
type msTeamsTypeNotifier struct {
	webHookURL string
	data       bool
}

// newMsTeamsNotifier creates a Teams notifier from command-line flags.
//
// Parameters:
//   - cmd: Cobra command with flags.
//
// Returns:
//   - types.ConvertibleNotifier: New Teams notifier instance.
func newMsTeamsNotifier(cmd *cobra.Command) types.ConvertibleNotifier {
	flags := cmd.Flags()

	// Extract and validate webhook URL.
	webHookURL, _ := flags.GetString("notification-msteams-hook")
	clog := logrus.WithField("url", webHookURL)

	if len(webHookURL) == 0 {
		clog.Fatal(
			"Microsoft Teams webhook URL is empty; required argument --notification-msteams-hook(cli) or WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL(env) missing",
		)
	}

	// Get data inclusion flag.
	withData, _ := flags.GetBool("notification-msteams-data")
	clog.WithField("with_data", withData).Debug("Initializing Microsoft Teams notifier")

	return &msTeamsTypeNotifier{
		webHookURL: webHookURL,
		data:       withData,
	}
}

// GetURL generates the Teams service URL from the notifier’s webhook.
//
// Parameters:
//   - c: Cobra command (unused here).
//
// Returns:
//   - string: Teams service URL.
//   - error: Non-nil if parsing or config fails, nil on success.
func (n *msTeamsTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.WithField("url", n.webHookURL)
	clog.Debug("Generating Microsoft Teams service URL")

	// Parse the webhook URL.
	webhookURL, err := url.Parse(n.webHookURL)
	if err != nil {
		clog.WithError(err).Debug("Failed to parse Microsoft Teams webhook URL")

		return "", fmt.Errorf("%w: %w", errParseWebhookFailed, err)
	}

	clog.Debug("Parsed Microsoft Teams webhook URL")

	// Create Teams config from webhook.
	config, err := teams.ConfigFromWebhookURL(*webhookURL)
	if err != nil {
		clog.WithError(err).
			Debug("Failed to create Microsoft Teams config from webhook URL")

		return "", fmt.Errorf("%w: %w", errConfigWebhookFailed, err)
	}

	// Set predefined color and generate URL.
	config.Color = ColorHex

	urlStr := config.GetURL().String()
	clog.WithField("service_url", urlStr).Debug("Generated Microsoft Teams service URL")

	return urlStr, nil
}
