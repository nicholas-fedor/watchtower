package gotify

import (
	"errors"
	"net/url"
	"strings"

	"github.com/rs/zerolog"

	shoutrrrGotify "github.com/nicholas-fedor/shoutrrr/pkg/services/push/gotify"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
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

// URL converts the legacy Gotify settings to a Shoutrrr Gotify URL.
//
// Parameters:
//   - log: Logger for configuration diagnostics.
//   - cfg: Deprecated Gotify server settings (from process config or flags).
//
// Returns:
//   - string: Gotify service URL.
//   - error: Non-nil when the token or the server URL is empty. Also non-nil
//     when the server URL is not an HTTP or HTTPS URL, does not parse or has
//     no host name.
//
// Deprecated: Legacy gotify notifier is deprecated.
// Use --notification-url with a gotify:// URL instead.
//
// TODO: Remove URL for the v2 release.
//
//nolint:godox
func URL(log *zerolog.Logger, cfg notifyConfig.Legacy) (string, error) {
	// Check the token first so a rejected configuration logs no URL warning.
	if cfg.GotifyToken == "" {
		return "", errEmptyToken
	}

	apiURL, err := parseGotifyURL(log, cfg.GotifyURL)
	if err != nil {
		return "", err
	}

	clog := log.With().
		Str("url", redact.ServiceURL(cfg.GotifyURL)).
		Bool("skip_verify", cfg.GotifyTLSSkipVerify).
		Logger()
	clog.Debug().Msg("Generating Gotify service URL")

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Int("token_length", len(cfg.GotifyToken)).
			Msg("Gotify notifier token loaded")
	}

	config := &shoutrrrGotify.Config{
		Host:          apiURL.Host,
		Path:          apiURL.Path,
		DisableTLS:    apiURL.Scheme == "http",
		SkipTLSVerify: cfg.GotifyTLSSkipVerify,
		Token:         cfg.GotifyToken,
	}

	urlStr := config.GetURL().String()

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Bool("disable_tls", config.DisableTLS).
			Str("service_url", redact.ServiceURL(urlStr)).
			Msg("Generated Gotify service URL")
	} else {
		clog.Debug().
			Bool("disable_tls", config.DisableTLS).
			Msg("Generated Gotify service URL")
	}

	return urlStr, nil
}

// parseGotifyURL checks and parses a Gotify server URL. It warns when a URL
// that parses uses plain HTTP.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - gotifyURL: URL value from resolved configuration or flags.
//
// Returns:
//   - *url.URL: The parsed server URL.
//   - error: Non-nil when the URL is empty or not an HTTP or HTTPS URL. Also
//     non-nil when it does not parse or has no host name.
//
// Deprecated: This function is part of the legacy gotify notifier and will be removed
// for the v2 release. Use --notification-url with a gotify:// URL instead.
func parseGotifyURL(log *zerolog.Logger, gotifyURL string) (*url.URL, error) {
	if gotifyURL == "" {
		return nil, errEmptyURL
	}

	if !strings.HasPrefix(gotifyURL, "http://") && !strings.HasPrefix(gotifyURL, "https://") {
		return nil, errURLScheme
	}

	clog := log.With().
		Str("flag", "notification-gotify-url").
		Str("url", redact.ServiceURL(gotifyURL)).
		Logger()

	// A parse error quotes the URL, which can carry credentials. Neither the
	// log nor the returned error includes it.
	apiURL, err := url.Parse(gotifyURL)
	if err != nil {
		clog.Debug().Msg("Failed to parse Gotify URL")

		return nil, errInvalidURL
	}

	// A server URL without a host name and path, such as "https://", would give
	// a Gotify URL that Shoutrrr reads with the token as its host. Shoutrrr
	// would then reject the token rather than the missing host. Checking every
	// URL here gives one clear error, including for the URLs that Shoutrrr
	// reports as missing a host.
	if apiURL.Hostname() == "" {
		clog.Debug().Msg("Gotify URL has no host")

		return nil, errInvalidURL
	}

	// Warn only for a URL that is used, so a rejected URL logs no warning.
	if apiURL.Scheme == "http" {
		clog.Warn().Msg("Using an HTTP URL for Gotify is insecure")
	}

	clog.Debug().
		Str("scheme", apiURL.Scheme).
		Msg("Validated Gotify URL")

	return apiURL, nil
}
