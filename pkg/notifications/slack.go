// Package notifications provides mechanisms for sending notifications via various services.
// This file implements Slack notification functionality with webhook support.
package notifications

import (
	"fmt"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/discord"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/slack"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	slackType = "slack"
)

// slackTypeNotifier handles Slack notifications via webhook.
// It supports customization with username, channel, and icon settings.
type slackTypeNotifier struct {
	HookURL   string
	Username  string
	Channel   string
	IconEmoji string
	IconURL   string
}

// newSlackNotifier creates a new Slack notifier from command-line flags.
// It initializes webhook URL, username, channel, and icon preferences.
func newSlackNotifier(c *cobra.Command) types.ConvertibleNotifier {
	flags := c.Flags()

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
// It detects Discord wrappers and constructs the appropriate service URL.
func (s *slackTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.WithField("hook_url", s.HookURL)
	clog.Debug("Generating Slack service URL")

	trimmedURL := strings.TrimRight(s.HookURL, "/")
	trimmedURL = strings.TrimPrefix(trimmedURL, "https://")
	parts := strings.Split(trimmedURL, "/")

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

	webhookToken := strings.Replace(s.HookURL, "https://hooks.slack.com/services/", "", 1)

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

	if err := conf.Token.SetFromProp(webhookToken); err != nil {
		clog.WithError(err).Debug("Failed to set Slack webhook token")

		return "", fmt.Errorf("failed to set Slack webhook token: %w", err)
	}

	urlStr := conf.GetURL().String()
	clog.WithField("service_url", urlStr).Debug("Generated Slack service URL")

	return urlStr, nil
}
