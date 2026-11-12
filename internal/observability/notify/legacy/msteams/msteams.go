package msteams

import (
	"errors"

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

// errEmptyWebhookURL indicates that no Microsoft Teams webhook URL is set.
var errEmptyWebhookURL = errors.New("the notification-msteams-hook configuration option is empty")

// URL converts the legacy Microsoft Teams webhook setting to a Shoutrrr Teams
// URL.
//
// Shoutrrr checks the webhook URL when it reads the returned URL. It rejects
// one that is not a signed Power Automate or Azure Logic Apps workflow URL.
//
// Parameters:
//   - log: Logger for configuration diagnostics.
//   - cfg: Deprecated MSTeams webhook settings (from process config or flags).
//
// Returns:
//   - string: Teams service URL.
//   - error: Non-nil when the webhook URL is empty.
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
