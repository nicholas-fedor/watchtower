package notifications

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/discord"
	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/slack"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
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

// slackWebhookChannel is the shoutrrr Slack channel that posts to the
// webhook's own channel.
const slackWebhookChannel = "webhook"

// slackChannelNotConvertedMessage is logged when a legacy Slack channel cannot
// be carried into the shoutrrr URL.
const slackChannelNotConvertedMessage = "Legacy Slack channel is not a channel ID, so the webhook's own channel is used"

// slackChannelIDPattern matches a Slack conversation ID, such as C8UJ12P4P.
//
// Slack conversation IDs start with C, G, or D and use uppercase letters and
// digits. Channel names are always lowercase, so a name never matches. A
// shoutrrr Slack URL holds the channel as its host, which can carry an ID but
// not a #name or @user.
var slackChannelIDPattern = regexp.MustCompile(`^[CGD][A-Z0-9]{8,}$`)

// discordAPIVersionPattern matches a Discord API version path segment, such as v10.
var discordAPIVersionPattern = regexp.MustCompile(`^v[0-9]+$`)

// errInvalidDiscordWebhookURL indicates a Discord wrapper hook URL that does
// not have the form https://discord.com/api/webhooks/<id>/<token>/slack.
var errInvalidDiscordWebhookURL = errors.New(
	"invalid Discord webhook URL, expected https://discord.com/api/webhooks/<id>/<token>/slack",
)

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
	log       *zerolog.Logger
}

// newSlackNotifier creates a Slack notifier from resolved legacy settings.
//
// Parameters:
//   - log: Logger for configuration diagnostics and fatal validation errors.
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
func newSlackNotifier(log *zerolog.Logger, legacy notifyConfig.Legacy) types.ConvertibleNotifier {
	hookURL := legacy.SlackHookURL
	userName := legacy.SlackIdentifier
	channel := legacy.SlackChannel
	emoji := legacy.SlackIconEmoji
	iconURL := legacy.SlackIconURL

	if hookURL == "" {
		log.Fatal().Msg("Slack hook URL is empty.")
	}

	log.Debug().
		Str("hook_url", redactServiceURL(hookURL)).
		Str("username", userName).
		Str("channel", channel).
		Str("emoji", emoji).
		Str("icon_url", iconURL).
		Msg("Initializing Slack notifier")

	return &slackTypeNotifier{
		HookURL:   hookURL,
		Username:  userName,
		Channel:   channel,
		IconEmoji: emoji,
		IconURL:   iconURL,
		log:       log,
	}
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
	clog := s.log
	clog.Debug().Msg("Generating Slack service URL")

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("hook_url", redactServiceURL(s.HookURL)).
			Msg("Slack hook URL loaded")
	}

	// Normalize URL and split its path into parts. The query string and fragment
	// are dropped first, so their contents never count as path segments.
	trimmedURL, _, _ := strings.Cut(s.HookURL, "?")
	trimmedURL, _, _ = strings.Cut(trimmedURL, "#")
	trimmedURL = strings.TrimRight(trimmedURL, "/")
	trimmedURL = strings.TrimPrefix(trimmedURL, "https://")
	parts := strings.Split(trimmedURL, "/")

	// Handle Discord wrapper URLs.
	if parts[0] == "discord.com" || parts[0] == "discordapp.com" {
		clog.Debug().Msg("Detected a discord slack wrapper URL, using shoutrrr discord service")

		webhookID, token, ok := discordWebhookCredentials(parts)
		if !ok {
			clog.Debug().Msg("Discord wrapper URL does not have the expected form")

			return "", errInvalidDiscordWebhookURL
		}

		conf := &discord.Config{
			WebhookID:  webhookID,
			Token:      token,
			Color:      colorInt,
			SplitLines: true,
			Username:   s.Username,
		}

		if s.IconURL != "" {
			conf.Avatar = s.IconURL
		}

		urlStr := conf.GetURL().String()
		if clog.GetLevel() <= zerolog.TraceLevel {
			clog.Trace().
				Str("service_url", redactServiceURL(urlStr)).
				Msg("Generated Discord service URL")
		} else {
			clog.Debug().Msg("Generated Discord service URL")
		}

		return urlStr, nil
	}

	// Extract Slack webhook token.
	webhookToken := strings.Replace(s.HookURL, "https://hooks.slack.com/services/", "", 1)

	// Configure Slack settings.
	conf := &slack.Config{
		BotName: s.Username,
		Color:   colorHex,
		Channel: s.webhookChannel(),
	}

	if s.IconURL != "" {
		conf.Icon = s.IconURL
	} else if s.IconEmoji != "" {
		conf.Icon = s.IconEmoji
	}

	// Set webhook token.
	err := conf.Token.SetFromProp(webhookToken)
	if err != nil {
		clog.Debug().Err(err).Msg("Failed to set Slack webhook token")

		return "", fmt.Errorf("failed to set Slack webhook token: %w", err)
	}

	urlStr := conf.GetURL().String()
	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("service_url", redactServiceURL(urlStr)).
			Msg("Generated Slack service URL")
	} else {
		clog.Debug().Msg("Generated Slack service URL")
	}

	return urlStr, nil
}

// discordWebhookCredentials returns the webhook ID and token from the path
// segments of a Discord wrapper hook URL. The path must be
// /api/webhooks/<id>/<token>/slack, or /api/<version>/webhooks/<id>/<token>/slack
// with a version segment such as v10.
//
// Parameters:
//   - parts: The URL path split on "/", starting with the host, without a query
//     string or fragment.
//
// Returns:
//   - webhookID: The webhook ID.
//   - token: The webhook token.
//   - ok: False when the path does not have one of those forms or the ID or
//     token is empty.
//
//nolint:nonamedreturns // Same-type returns need names.
func discordWebhookCredentials(parts []string) (webhookID, token string, ok bool) {
	// Drop the host, then an optional version segment after "api".
	path := parts[1:]
	if len(path) == 6 && path[0] == "api" && discordAPIVersionPattern.MatchString(path[1]) {
		path = append([]string{"api"}, path[2:]...)
	}

	if len(path) != 5 || path[0] != "api" || path[1] != "webhooks" || path[4] != "slack" {
		return "", "", false
	}

	webhookID, token = path[2], path[3]
	if webhookID == "" || token == "" {
		return "", "", false
	}

	return webhookID, token, true
}

// webhookChannel returns the channel for the shoutrrr Slack URL.
//
// A legacy channel given as a Slack conversation ID is carried over, which a
// legacy custom-integration webhook uses as its channel override. Any other
// value cannot be held in the URL, so the webhook's own channel is used and a
// warning names the value.
//
// Returns:
//   - string: The channel ID, or the webhook placeholder for the webhook's own channel.
func (s *slackTypeNotifier) webhookChannel() string {
	if s.Channel == "" {
		return slackWebhookChannel
	}

	if slackChannelIDPattern.MatchString(s.Channel) {
		return s.Channel
	}

	s.log.Warn().
		Str("channel", s.Channel).
		Msg(slackChannelNotConvertedMessage)

	return slackWebhookChannel
}
