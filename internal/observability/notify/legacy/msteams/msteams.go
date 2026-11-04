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
	"github.com/nicholas-fedor/watchtower/pkg/types"
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

// msTeamsTypeNotifier handles Microsoft Teams notifications via webhook.
//
// Deprecated: Legacy msteams notifier is deprecated.
// Use --notification-url with a teams:// URL instead.
//
// TODO: Remove msTeamsTypeNotifier for the v2 release.
//
//nolint:godox
type msTeamsTypeNotifier struct {
	webHookURL string
	log        *zerolog.Logger
}

// New creates a Teams notifier from resolved legacy settings.
//
// Parameters:
//   - log: Logger for configuration diagnostics.
//   - cfg: Deprecated MSTeams webhook settings (from process config or flags).
//
// Returns:
//   - types.ConvertibleNotifier: New Teams notifier instance.
//   - error: Non-nil when the webhook URL is empty.
//
// Deprecated: Legacy msteams notifier is deprecated.
// Use --notification-url with a teams:// URL instead.
//
// TODO: Remove New for the v2 release.
//
//nolint:godox
func New(log *zerolog.Logger, cfg notifyConfig.Legacy) (types.ConvertibleNotifier, error) {
	webHookURL := cfg.MSTeamsHook
	if webHookURL == "" {
		return nil, errEmptyWebhookURL
	}

	return &msTeamsTypeNotifier{webHookURL: webHookURL, log: log}, nil
}

// GetURL generates the Teams service URL from the notifier's webhook.
//
// Returns:
//   - string: Teams service URL.
//   - error: Non-nil if parsing fails, nil on success.
//
// Deprecated: This method is part of the legacy msteams notifier and will be removed
// for the v2 release. Use --notification-url with a teams:// URL instead.
func (n *msTeamsTypeNotifier) GetURL() (string, error) {
	clog := n.log
	clog.Debug().Msg("Generating Microsoft Teams service URL")

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("url", redact.ServiceURL(n.webHookURL)).
			Msg("Microsoft Teams webhook URL loaded")
	}

	// Validate the webhook URL is parseable and absolute.
	// A parse error quotes the URL, which carries the webhook's credentials.
	// Neither the log nor the returned error includes it.
	parsed, err := url.Parse(n.webHookURL)
	if err != nil {
		clog.Debug().Msg("Failed to parse Microsoft Teams webhook URL")

		return "", errParseWebhookFailed
	}

	if parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("%w: expected https URL", errParseWebhookFailed)
	}

	// Create Teams config with the full webhook URL as the host.
	config := &teams.Config{
		Host:  n.webHookURL,
		Color: legacy.ColorHex,
	}

	urlStr := config.GetURL().String()
	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("service_url", redact.ServiceURL(urlStr)).
			Msg("Generated Microsoft Teams service URL")
	} else {
		clog.Debug().Msg("Generated Microsoft Teams service URL")
	}

	return urlStr, nil
}
