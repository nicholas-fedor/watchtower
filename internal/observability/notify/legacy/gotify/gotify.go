package gotify

import (
	"errors"
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

// Errors for Gotify notification configuration.
var (
	// errEmptyURL indicates that no Gotify server URL is set.
	errEmptyURL = errors.New("the notification-gotify-url configuration option is empty")
	// errInvalidURL indicates a server URL that does not parse.
	errInvalidURL = errors.New("the notification-gotify-url configuration option is not a valid URL")
	// errURLScheme indicates a Gotify server URL that is not an HTTP or HTTPS URL.
	errURLScheme = errors.New(`the notification-gotify-url configuration option must start with "http://" or "https://"`)
	// errEmptyToken indicates that no Gotify application token is set.
	errEmptyToken = errors.New("the notification-gotify-token configuration option is empty")
)

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
	log                 *zerolog.Logger
	gotifyURL           string // Gotify server URL.
	gotifyAppToken      string // Gotify application token.
	gotifySkipTLSVerify bool   // Skip TLS verification if true.
}

// New creates a Gotify notifier from resolved legacy settings.
//
// Parameters:
//   - log: Logger for configuration diagnostics.
//   - cfg: Deprecated Gotify server settings (from process config or flags).
//
// Returns:
//   - types.ConvertibleNotifier: New Gotify notifier instance.
//   - error: Non-nil when the server URL is empty or not an HTTP or HTTPS URL.
//     Also non-nil when the token is empty.
//
// Deprecated: Legacy gotify notifier is deprecated.
// Use --notification-url with a gotify:// URL instead.
//
// TODO: Remove New for the v2 release.
//
//nolint:godox
func New(log *zerolog.Logger, cfg notifyConfig.Legacy) (types.ConvertibleNotifier, error) {
	apiURL := cfg.GotifyURL
	token := cfg.GotifyToken
	skipVerify := cfg.GotifyTLSSkipVerify

	// Check the token first so a rejected configuration logs no URL warning.
	if token == "" {
		return nil, errEmptyToken
	}

	err := validateGotifyURL(log, apiURL)
	if err != nil {
		return nil, err
	}

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
	}, nil
}

// validateGotifyURL checks a Gotify server URL. It warns when the URL uses
// plain HTTP.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - gotifyURL: URL value from resolved configuration or flags.
//
// Returns:
//   - error: Non-nil when the URL is empty or not an HTTP or HTTPS URL.
//
// Deprecated: This function is part of the legacy gotify notifier and will be removed
// for the v2 release. Use --notification-url with a gotify:// URL instead.
func validateGotifyURL(log *zerolog.Logger, gotifyURL string) error {
	if gotifyURL == "" {
		return errEmptyURL
	}

	if !strings.HasPrefix(gotifyURL, "http://") && !strings.HasPrefix(gotifyURL, "https://") {
		return errURLScheme
	}

	clog := log.With().
		Str("flag", "notification-gotify-url").
		Str("url", redact.ServiceURL(gotifyURL)).
		Logger()

	// Warn if using insecure HTTP.
	if strings.HasPrefix(gotifyURL, "http://") {
		clog.Warn().Msg("Using an HTTP URL for Gotify is insecure")
	}

	clog.Debug().
		Str("scheme", strings.Split(gotifyURL, ":")[0]).
		Msg("Validated Gotify URL")

	return nil
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
	// A parse error quotes the URL, which can carry credentials. Neither the
	// log nor the returned error includes it.
	apiURL, err := url.Parse(n.gotifyURL)
	if err != nil {
		clog.Debug().Msg("Failed to parse Gotify URL")

		return "", errInvalidURL
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
