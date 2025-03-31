// Package notifications provides mechanisms for sending notifications via various services.
// This file implements Microsoft Teams notification functionality.
package notifications

import (
	"fmt"
	"net/url"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/teams"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	msTeamsType = "msteams"
)

// msTeamsTypeNotifier handles Microsoft Teams notifications via webhook.
// It supports optional data inclusion for detailed messages.
type msTeamsTypeNotifier struct {
	webHookURL string
	data       bool
}

// newMsTeamsNotifier creates a new Microsoft Teams notifier from command-line flags.
// It validates the webhook URL and sets data inclusion preference.
func newMsTeamsNotifier(cmd *cobra.Command) types.ConvertibleNotifier {
	flags := cmd.Flags()

	webHookURL, _ := flags.GetString("notification-msteams-hook")
	if len(webHookURL) == 0 {
		logrus.Fatal(
			"Required argument --notification-msteams-hook(cli) or WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL(env) is empty.",
		)
	}

	withData, _ := flags.GetBool("notification-msteams-data")
	n := &msTeamsTypeNotifier{
		webHookURL: webHookURL,
		data:       withData,
	}

	return n
}

// GetURL generates the Microsoft Teams webhook URL for the notifier.
// It parses the webhook and constructs the service URL with predefined color settings.
func (n *msTeamsTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	webhookURL, err := url.Parse(n.webHookURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse Microsoft Teams webhook URL: %w", err)
	}
	logrus.Debugf("Parsed webhook URL: %s", n.webHookURL)
	config, err := teams.ConfigFromWebhookURL(*webhookURL)
	if err != nil {
		logrus.Debugf("Config error with URL: %s", n.webHookURL)
		return "", fmt.Errorf("failed to create Microsoft Teams config from webhook URL: %w", err)
	}

	config.Color = ColorHex

	return config.GetURL().String(), nil
}
