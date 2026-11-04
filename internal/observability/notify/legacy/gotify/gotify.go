package gotify

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rs/zerolog"

	shoutrrrGotify "github.com/nicholas-fedor/shoutrrr/pkg/services/push/gotify"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Type is the identifier for Gotify notifications.
//
// Deprecated: Legacy gotify notification type is deprecated.
// Use --notification-url with a gotify:// URL instead.
//
// TODO: Remove Type constant for the v2 release.
//
//nolint:godox
const Type = "gotify"

// gotifyTypeNotifier handles Gotify notifications.
//
// It configures URL, token, and TLS settings.
//
// Deprecated: Legacy gotify notifier is deprecated.
// Use --notification-url with a gotify:// URL instead.
//
// TODO: Remove gotifyTypeNotifier for the v2 release.
//
//nolint:godox
type gotifyTypeNotifier struct {
	gotifyURL           string // Gotify server URL.
	gotifyAppToken      string // Gotify application token.
	gotifySkipTLSVerify bool   // Skip TLS verification if true.
	log                 *zerolog.Logger
}

// New creates a Gotify notifier from resolved legacy settings.
//
// Parameters:
//   - log: Logger for configuration diagnostics and fatal validation errors.
//   - cfg: Deprecated Gotify server settings (from process config or flags).
//
// Returns:
//   - types.ConvertibleNotifier: New Gotify notifier instance.
//
// Deprecated: Legacy gotify notifier is deprecated.
// Use --notification-url with a gotify:// URL instead.
//
// TODO: Remove New for the v2 release.
//
//nolint:godox
func New(log *zerolog.Logger, cfg notifyConfig.Legacy) types.ConvertibleNotifier {
	apiURL := requireGotifyURL(log, cfg.GotifyURL)
	token := requireGotifyToken(log, cfg.GotifyToken)
	skipVerify := cfg.GotifyTLSSkipVerify

	clog := log.With().
		Str("url", redact.ServiceURL(apiURL)).
		Bool("skip_verify", skipVerify).
		Logger()
	clog.Debug().Msg("Initializing Gotify notifier")

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Int("token_length", len(token)).
			Msg("Gotify notifier token loaded")
	}

	return &gotifyTypeNotifier{
		gotifyURL:           apiURL,
		gotifyAppToken:      token,
		gotifySkipTLSVerify: skipVerify,
		log:                 log,
	}
}

// requireGotifyToken validates a Gotify token.
//
// Parameters:
//   - log: Logger for fatal validation errors.
//   - gotifyToken: Token value from resolved configuration or flags.
//
// Returns:
//   - string: Token value (fatal if empty).
//
// Deprecated: This function is part of the legacy gotify notifier and will be removed
// for the v2 release. Use --notification-url with a gotify:// URL instead.
func requireGotifyToken(log *zerolog.Logger, gotifyToken string) string {
	clog := log.With().Str("flag", "notification-gotify-token").Logger()

	// Fatal error if token is missing.
	if len(gotifyToken) < 1 {
		clog.Fatal().Msg("Gotify token is empty.")
	}

	clog.Debug().Int("token_length", len(gotifyToken)).Msg("Retrieved Gotify token")

	return gotifyToken
}

// requireGotifyURL validates a Gotify URL.
//
// Parameters:
//   - log: Logger for fatal validation errors.
//   - gotifyURL: URL value from resolved configuration or flags.
//
// Returns:
//   - string: Validated URL (fatal if empty or malformed).
//
// Deprecated: This function is part of the legacy gotify notifier and will be removed
// for the v2 release. Use --notification-url with a gotify:// URL instead.
func requireGotifyURL(log *zerolog.Logger, gotifyURL string) string {
	clog := log.With().
		Str("flag", "notification-gotify-url").
		Str("url", redact.ServiceURL(gotifyURL)).
		Logger()

	// Fatal error if URL is missing.
	if len(gotifyURL) < 1 {
		clog.Fatal().Msg("Gotify URL is empty")
	}

	// Validate URL scheme.
	if !strings.HasPrefix(gotifyURL, "http://") && !strings.HasPrefix(gotifyURL, "https://") {
		clog.Fatal().Msg("Gotify URL must start with \"http://\" or \"https://\"")
	}

	// Warn if using insecure HTTP.
	if strings.HasPrefix(gotifyURL, "http://") {
		clog.Warn().Msg("Using an HTTP URL for Gotify is insecure")
	}

	clog.Debug().
		Str("scheme", strings.Split(gotifyURL, ":")[0]).
		Msg("Validated Gotify URL")

	return gotifyURL
}

// GetURL generates the Gotify service URL from the notifier's configuration.
//
// Returns:
//   - string: Gotify service URL.
//   - error: Non-nil if URL parsing fails, nil on success.
//
// Deprecated: This method is part of the legacy gotify notifier and will be removed
// for the v2 release. Use --notification-url with a gotify:// URL instead.
func (n *gotifyTypeNotifier) GetURL() (string, error) {
	clog := n.log
	clog.Debug().Msg("Generating Gotify service URL")

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("url", redact.ServiceURL(n.gotifyURL)).
			Msg("Gotify API URL loaded")
	}

	// Parse the API URL.
	apiURL, err := url.Parse(n.gotifyURL)
	if err != nil {
		clog.Debug().Err(err).Msg("Failed to parse Gotify URL")

		return "", fmt.Errorf("failed to generate Gotify URL: %w", err)
	}

	// Configure Gotify settings.
	config := &shoutrrrGotify.Config{
		Host:          apiURL.Host,
		Path:          apiURL.Path,
		DisableTLS:    apiURL.Scheme == "http",
		SkipTLSVerify: n.gotifySkipTLSVerify,
		Token:         n.gotifyAppToken,
	}

	urlStr := config.GetURL().String()

	clog.Debug().
		Bool("disable_tls", apiURL.Scheme == "http").
		Bool("skip_tls_verify", n.gotifySkipTLSVerify).
		Msg("Generated Gotify service URL")

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("service_url", redact.ServiceURL(urlStr)).
			Msg("Generated Gotify service URL")
	}

	return urlStr, nil
}
