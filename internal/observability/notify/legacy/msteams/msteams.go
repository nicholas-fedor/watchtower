package msteams

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/teams"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/legacy"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
)

// Type is the identifier for Microsoft Teams notifications.
//
// Deprecated: Legacy msteams notification type is deprecated.
// Use --notification-url with a teams:// URL instead.
//
// TODO: Remove Type constant for the v2 release.
//
//nolint:godox
const Type = "msteams"

// Errors for Microsoft Teams notification configuration.
var (
	// errParseWebhookFailed indicates a failure to parse the Microsoft Teams webhook URL.
	errParseWebhookFailed = errors.New("failed to parse Microsoft Teams webhook URL")
	// errEmptyWebhookURL indicates that no Microsoft Teams webhook URL is set.
	errEmptyWebhookURL = errors.New("the notification-msteams-hook configuration option is empty")
)

// URL converts the legacy Microsoft Teams webhook setting to a Shoutrrr Teams
// URL.
//
// Parameters:
//   - log: Logger for configuration diagnostics.
//   - cfg: Deprecated MSTeams webhook settings (from process config or flags).
//
// Returns:
//   - string: Teams service URL.
//   - error: Non-nil when the webhook URL is empty. Also non-nil when it does
//     not parse or is not an absolute https URL.
//
// Deprecated: Legacy msteams notifier is deprecated.
// Use --notification-url with a teams:// URL instead.
//
// TODO: Remove URL for the v2 release.
//
//nolint:godox
func URL(log *zerolog.Logger, cfg notifyConfig.Legacy) (string, error) {
	webHookURL := cfg.MSTeamsHook
	if webHookURL == "" {
		return "", errEmptyWebhookURL
	}

	log.Debug().Msg("Generating Microsoft Teams service URL")

	if log.GetLevel() <= zerolog.TraceLevel {
		log.Trace().
			Str("url", redact.ServiceURL(webHookURL)).
			Msg("Microsoft Teams webhook URL loaded")
	}

	// Validate the webhook URL is parseable and absolute.
	// A parse error quotes the URL, which carries the webhook's credentials.
	// Neither the log nor the returned error includes it.
	parsed, err := url.Parse(webHookURL)
	if err != nil {
		log.Debug().Msg("Failed to parse Microsoft Teams webhook URL")

		return "", errParseWebhookFailed
	}

	if parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("%w: expected https URL", errParseWebhookFailed)
	}

	// Create Teams config with the full webhook URL as the host.
	config := &teams.Config{
		Host:  webHookURL,
		Color: legacy.ColorHex,
	}

	urlStr := config.GetURL().String()
	if log.GetLevel() <= zerolog.TraceLevel {
		log.Trace().
			Str("service_url", redact.ServiceURL(urlStr)).
			Msg("Generated Microsoft Teams service URL")
	} else {
		log.Debug().Msg("Generated Microsoft Teams service URL")
	}

	return urlStr, nil
}
