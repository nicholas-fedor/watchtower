// Package notifications provides mechanisms for sending notifications via various services.
// This file implements Slack notification functionality with webhook support.
package notifications

import (
	"fmt"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/discord"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/slack"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// slackType is the identifier for Slack notifications.
const slackType = "slack"

// slackTypeNotifier handles Slack notifications via webhook.
//
// It supports custom username, channel, and icons.
type slackTypeNotifier struct {
	HookURL   string // Slack webhook URL.
	Username  string // Notification username.
	Channel   string // Target channel (unused in webhook mode).
	IconEmoji string // Emoji icon for messages.
	IconURL   string // URL icon for messages.
}

// newSlackNotifier creates a Slack notifier from command-line flags.
//
// Parameters:
//   - c: Cobra command with flags.
//
// Returns:
//   - types.ConvertibleNotifier: New Slack notifier instance.
func newSlackNotifier(c *cobra.Command) types.ConvertibleNotifier {
	flags := c.Flags()

	// Extract Slack configuration from flags.
	hookURL, _ := flags.GetString("notification-slack-hook-url")
	userName, _ := flags.GetString("notification-slack-identifier")
	channel, _ := flags.GetString("notification-slack-channel")
	emoji, _ := flags.GetString("notification-slack-icon-emoji")
	iconURL, _ := flags.GetString("notification-slack-icon-url")

	clog := logrus.WithFields(logrus.Fields{
		"hook_url": hookURL,
		"username": userName,
		"channel":  channel,
		"emoji":    emoji,
		"icon_url": iconURL,
	})
	clog.Debug("Initializing Slack notifier")

	notifier := &slackTypeNotifier{
		HookURL:   hookURL,
		Username:  userName,
		Channel:   channel,
		IconEmoji: emoji,
		IconURL:   iconURL,
	}

	return notifier
}

// GetURL generates the Slack webhook URL for the notifier.
//
// Parameters:
//   - c: Cobra command (unused here).
//
// Returns:
//   - string: Service URL (Slack or Discord).
//   - error: Non-nil if token parsing fails, nil on success.
func (s *slackTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.WithField("hook_url", s.HookURL)
	clog.Debug("Generating Slack service URL")

	// Normalize URL and split parts.
	trimmedURL := strings.TrimRight(s.HookURL, "/")
	trimmedURL = strings.TrimPrefix(trimmedURL, "https://")
	parts := strings.Split(trimmedURL, "/")

	// Handle Discord wrapper URLs.
	if parts[0] == "discord.com" || parts[0] == "discordapp.com" {
		clog.Debug("Detected a discord slack wrapper URL, using shoutrrr discord service")

		conf := &discord.Config{
			WebhookID:  parts[len(parts)-3],
			Token:      parts[len(parts)-2],
			Color:      ColorInt,
			SplitLines: true,
			Username:   s.Username,
		}

		if s.IconURL != "" {
			conf.Avatar = s.IconURL
		}

		urlStr := conf.GetURL().String()
		clog.WithField("service_url", urlStr).Debug("Generated Discord service URL")

		return urlStr, nil
	}

	// Extract Slack webhook token.
	webhookToken := strings.Replace(s.HookURL, "https://hooks.slack.com/services/", "", 1)

	// Configure Slack settings.
	conf := &slack.Config{
		BotName: s.Username,
		Color:   ColorHex,
		Channel: "webhook",
	}

	if s.IconURL != "" {
		conf.Icon = s.IconURL
	} else if s.IconEmoji != "" {
		conf.Icon = s.IconEmoji
	}

	// Set webhook token.
	if err := conf.Token.SetFromProp(webhookToken); err != nil {
		clog.WithError(err).Debug("Failed to set Slack webhook token")

		return "", fmt.Errorf("failed to set Slack webhook token: %w", err)
	}

	urlStr := conf.GetURL().String()
	clog.WithField("service_url", urlStr).Debug("Generated Slack service URL")

	return urlStr, nil
}
