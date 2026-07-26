package notifications

import (
	"fmt"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/discord"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/slack"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/config/notify"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// slackType is the identifier for Slack notifications.
//
// Deprecated: Legacy slack notification type is deprecated.
// Use --notification-url with a slack:// or discord:// URL instead.
//
// TODO: Remove slackType constant for the v2 release.
//
//nolint:godox
const slackType = "slack"

// slackTypeNotifier handles Slack notifications via webhook.
//
// It supports custom username, channel, and icons.
//
// Deprecated: Legacy slack notifier is deprecated.
// Use --notification-url with a slack:// or discord:// URL instead.
//
// TODO: Remove slackTypeNotifier for the v2 release.
//
//nolint:godox
type slackTypeNotifier struct {
	HookURL   string // Slack webhook URL.
	Username  string // Notification username.
	Channel   string // Target channel (unused in webhook mode).
	IconEmoji string // Emoji icon for messages.
	IconURL   string // URL icon for messages.
}

// newSlackNotifier creates a Slack notifier from resolved legacy settings.
//
// Parameters:
//   - legacy: Deprecated Slack/Discord webhook settings (from process config or flags).
//
// Returns:
//   - types.ConvertibleNotifier: New Slack notifier instance.
//
// Deprecated: Legacy slack notifier is deprecated.
// Use --notification-url with a slack:// or discord:// URL instead.
//
// TODO: Remove newSlackNotifier for the v2 release.
//
//nolint:godox
func newSlackNotifier(legacy notifyConfig.Legacy) types.ConvertibleNotifier {
	hookURL := legacy.SlackHookURL
	userName := legacy.SlackIdentifier
	channel := legacy.SlackChannel
	emoji := legacy.SlackIconEmoji
	iconURL := legacy.SlackIconURL

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
//
// Deprecated: This method is part of the legacy slack notifier and will be removed
// for the v2 release. Use --notification-url with a slack:// URL instead.
func (s *slackTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.NewEntry(logrus.StandardLogger())
	clog.Debug("Generating Slack service URL")

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("hook_url", redactServiceURL(s.HookURL)).
			Trace("Slack hook URL loaded")
	}

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
		if logrus.IsLevelEnabled(logrus.TraceLevel) {
			clog.WithField("service_url", redactServiceURL(urlStr)).
				Trace("Generated Discord service URL")
		} else {
			clog.Debug("Generated Discord service URL")
		}

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
	err := conf.Token.SetFromProp(webhookToken)
	if err != nil {
		clog.WithError(err).Debug("Failed to set Slack webhook token")

		return "", fmt.Errorf("failed to set Slack webhook token: %w", err)
	}

	urlStr := conf.GetURL().String()
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("service_url", redactServiceURL(urlStr)).
			Trace("Generated Slack service URL")
	} else {
		clog.Debug("Generated Slack service URL")
	}

	return urlStr, nil
}

// GetEntries returns nil for legacy notifiers.
//
// Returns:
//   - []*logrus.Entry: Always nil.
//
// Deprecated: This method is part of the legacy slack notifier and will be removed
// for the v2 release.
func (s *slackTypeNotifier) GetEntries() []*logrus.Entry {
	return nil
}

// SendFilteredEntries does nothing for legacy notifiers.
//
// Parameters:
//   - entries: Ignored.
//   - report: Ignored.
//
// Deprecated: This method is part of the legacy slack notifier and will be removed
// for the v2 release.
func (s *slackTypeNotifier) SendFilteredEntries(_ []*logrus.Entry, _ types.Report) {
	// Legacy notifiers do not support filtered entries.
}
