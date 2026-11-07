package slack

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/discord"
	"github.com/rs/zerolog"

	shoutrrrSlack "github.com/nicholas-fedor/shoutrrr/pkg/services/chat/slack"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/legacy"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
)

// Type is the identifier for Slack notifications.
//
// Deprecated: Legacy slack notification type is deprecated.
// Use --notification-url with a slack:// or discord:// URL instead.
//
// TODO: Remove Type constant for the v2 release.
//
//nolint:godox
const Type = "slack"

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

// errEmptyHookURL indicates that no Slack hook URL is set.
var errEmptyHookURL = errors.New("the notification-slack-hook-url configuration option is empty")

// errInvalidDiscordWebhookURL indicates a Discord wrapper hook URL that does
// not have the form https://discord.com/api/webhooks/<id>/<token>/slack.
var errInvalidDiscordWebhookURL = errors.New(
	"invalid Discord webhook URL, expected https://discord.com/api/webhooks/<id>/<token>/slack",
)

// URL converts the legacy Slack settings to a Shoutrrr URL.
//
// A hook URL on discord.com or discordapp.com becomes a Discord URL. Any other
// hook URL becomes a Slack URL.
//
// Parameters:
//   - log: Logger for configuration diagnostics.
//   - cfg: Deprecated Slack/Discord webhook settings (from process config or flags).
//
// Returns:
//   - string: Service URL (Slack or Discord).
//   - error: Non-nil when the hook URL is empty. Also non-nil when a Discord
//     hook URL does not have the wrapper form or a Slack webhook token does
//     not parse.
//
// Deprecated: Legacy slack notifier is deprecated.
// Use --notification-url with a slack:// or discord:// URL instead.
//
// TODO: Remove URL for the v2 release.
//
//nolint:godox
func URL(log *zerolog.Logger, cfg notifyConfig.Legacy) (string, error) {
	hookURL := cfg.SlackHookURL
	if hookURL == "" {
		return "", errEmptyHookURL
	}

	log.Debug().
		Str("hook_url", redact.ServiceURL(hookURL)).
		Str("username", cfg.SlackIdentifier).
		Str("channel", cfg.SlackChannel).
		Str("emoji", cfg.SlackIconEmoji).
		Str("icon_url", cfg.SlackIconURL).
		Msg("Generating Slack service URL")

	// Normalize URL and split its path into parts. The query string and fragment
	// are dropped first, so their contents never count as path segments.
	trimmedURL, _, _ := strings.Cut(hookURL, "?")
	trimmedURL, _, _ = strings.Cut(trimmedURL, "#")
	trimmedURL = strings.TrimRight(trimmedURL, "/")
	trimmedURL = strings.TrimPrefix(trimmedURL, "https://")
	parts := strings.Split(trimmedURL, "/")

	// Handle Discord wrapper URLs.
	if parts[0] == "discord.com" || parts[0] == "discordapp.com" {
		log.Debug().Msg("Detected a discord slack wrapper URL, using shoutrrr discord service")

		webhookID, token, ok := discordWebhookCredentials(parts)
		if !ok {
			log.Debug().Msg("Discord wrapper URL does not have the expected form")

			return "", errInvalidDiscordWebhookURL
		}

		conf := &discord.Config{
			WebhookID:  webhookID,
			Token:      token,
			Color:      legacy.ColorInt,
			SplitLines: true,
			Username:   cfg.SlackIdentifier,
		}

		if cfg.SlackIconURL != "" {
			conf.Avatar = cfg.SlackIconURL
		}

		urlStr := conf.GetURL().String()
		if log.GetLevel() <= zerolog.TraceLevel {
			log.Trace().
				Str("service_url", redact.ServiceURL(urlStr)).
				Msg("Generated Discord service URL")
		} else {
			log.Debug().Msg("Generated Discord service URL")
		}

		return urlStr, nil
	}

	// Extract Slack webhook token.
	webhookToken := strings.Replace(hookURL, "https://hooks.slack.com/services/", "", 1)

	// Configure Slack settings.
	conf := &shoutrrrSlack.Config{
		BotName: cfg.SlackIdentifier,
		Color:   legacy.ColorHex,
		Channel: webhookChannel(log, cfg.SlackChannel),
	}

	if cfg.SlackIconURL != "" {
		conf.Icon = cfg.SlackIconURL
	} else if cfg.SlackIconEmoji != "" {
		conf.Icon = cfg.SlackIconEmoji
	}

	// Set webhook token.
	err := conf.Token.SetFromProp(webhookToken)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to set Slack webhook token")

		return "", fmt.Errorf("failed to set Slack webhook token: %w", err)
	}

	urlStr := conf.GetURL().String()
	if log.GetLevel() <= zerolog.TraceLevel {
		log.Trace().
			Str("service_url", redact.ServiceURL(urlStr)).
			Msg("Generated Slack service URL")
	} else {
		log.Debug().Msg("Generated Slack service URL")
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
//   - ok: False when the path does not have one of those forms, or the ID or
//     token is not a valid path escape or is empty or only whitespace once
//     decoded.
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
	if blankPathSegment(webhookID) || blankPathSegment(token) {
		return "", "", false
	}

	return webhookID, token, true
}

// blankPathSegment reports whether a URL path segment carries no value. That is
// the case when it is not a valid path escape, or when it is empty or only
// whitespace once decoded, such as %20.
//
// Parameters:
//   - segment: The raw path segment.
//
// Returns:
//   - bool: True when the segment carries no value.
func blankPathSegment(segment string) bool {
	decoded, err := url.PathUnescape(segment)

	return err != nil || strings.TrimSpace(decoded) == ""
}

// webhookChannel returns the channel for the shoutrrr Slack URL.
//
// A legacy channel given as a Slack conversation ID is carried over, which a
// legacy custom-integration webhook uses as its channel override. Any other
// value cannot be held in the URL, so the webhook's own channel is used and a
// warning names the value.
//
// Parameters:
//   - log: Logger for the warning about a channel that is not an ID.
//   - channel: The legacy Slack channel setting.
//
// Returns:
//   - string: The channel ID, or the webhook placeholder for the webhook's own channel.
func webhookChannel(log *zerolog.Logger, channel string) string {
	if channel == "" {
		return slackWebhookChannel
	}

	if slackChannelIDPattern.MatchString(channel) {
		return channel
	}

	log.Warn().
		Str("channel", channel).
		Msg(slackChannelNotConvertedMessage)

	return slackWebhookChannel
}
