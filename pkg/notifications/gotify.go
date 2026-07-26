package notifications

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/push/gotify"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/config/notify"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// gotifyType is the identifier for Gotify notifications.
//
// Deprecated: Legacy gotify notification type is deprecated.
// Use --notification-url with a gotify:// URL instead.
//
// TODO: Remove gotifyType constant for the v2 release.
//
//nolint:godox
const gotifyType = "gotify"

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
	gotifyURL                string // Gotify server URL.
	gotifyAppToken           string // Gotify application token.
	gotifyInsecureSkipVerify bool   // Skip TLS verification if true.
}

// newGotifyNotifier creates a Gotify notifier from resolved legacy settings.
//
// Parameters:
//   - legacy: Deprecated Gotify server settings (from process config or flags).
//
// Returns:
//   - types.ConvertibleNotifier: New Gotify notifier instance.
//
// Deprecated: Legacy gotify notifier is deprecated.
// Use --notification-url with a gotify:// URL instead.
//
// TODO: Remove newGotifyNotifier for the v2 release.
//
//nolint:godox
func newGotifyNotifier(legacy notifyConfig.Legacy) types.ConvertibleNotifier {
	apiURL := requireGotifyURL(legacy.GotifyURL)
	token := requireGotifyToken(legacy.GotifyToken)
	skipVerify := legacy.GotifyTLSSkipVerify

	clog := logrus.WithFields(logrus.Fields{
		"url":         redactServiceURL(apiURL),
		"skip_verify": skipVerify,
	})
	clog.Debug("Initializing Gotify notifier")

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("token_length", len(token)).
			Trace("Gotify notifier token loaded")
	}

	return &gotifyTypeNotifier{
		gotifyURL:                apiURL,
		gotifyAppToken:           token,
		gotifyInsecureSkipVerify: skipVerify,
	}
}

// requireGotifyToken validates a Gotify token.
//
// Parameters:
//   - gotifyToken: Token value from resolved configuration or flags.
//
// Returns:
//   - string: Token value (fatal if empty).
//
// Deprecated: This function is part of the legacy gotify notifier and will be removed
// for the v2 release. Use --notification-url with a gotify:// URL instead.
func requireGotifyToken(gotifyToken string) string {
	clog := logrus.WithField("flag", "notification-gotify-token")

	// Fatal error if token is missing.
	if len(gotifyToken) < 1 {
		clog.Fatal(
			"Gotify token is empty; required argument --notification-gotify-token(cli) or WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN(env) is empty",
		)
	}

	clog.WithField("token_length", len(gotifyToken)).Debug("Retrieved Gotify token")

	return gotifyToken
}

// requireGotifyURL validates a Gotify URL.
//
// Parameters:
//   - gotifyURL: URL value from resolved configuration or flags.
//
// Returns:
//   - string: Validated URL (fatal if empty or malformed).
//
// Deprecated: This function is part of the legacy gotify notifier and will be removed
// for the v2 release. Use --notification-url with a gotify:// URL instead.
func requireGotifyURL(gotifyURL string) string {
	clog := logrus.WithFields(logrus.Fields{
		"flag": "notification-gotify-url",
		"url":  gotifyURL,
	})

	// Fatal error if URL is missing.
	if len(gotifyURL) < 1 {
		clog.Fatal(
			"Gotify URL is empty; required argument --notification-gotify-url(cli) or WATCHTOWER_NOTIFICATION_GOTIFY_URL(env) is empty",
		)
	}

	// Validate URL scheme.
	if !strings.HasPrefix(gotifyURL, "http://") && !strings.HasPrefix(gotifyURL, "https://") {
		clog.Fatal("Gotify URL must start with \"http://\" or \"https://\"")
	}

	// Warn if using insecure HTTP.
	if strings.HasPrefix(gotifyURL, "http://") {
		clog.Warn("Using an HTTP URL for Gotify is insecure")
	}

	clog.WithField("scheme", strings.Split(gotifyURL, ":")[0]).Debug("Validated Gotify URL")

	return gotifyURL
}

// GetURL generates the Gotify service URL from the notifier's configuration.
//
// Parameters:
//   - c: Cobra command (unused here).
//
// Returns:
//   - string: Gotify service URL.
//   - error: Non-nil if URL parsing fails, nil on success.
//
// Deprecated: This method is part of the legacy gotify notifier and will be removed
// for the v2 release. Use --notification-url with a gotify:// URL instead.
func (n *gotifyTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.NewEntry(logrus.StandardLogger())
	clog.Debug("Generating Gotify service URL")

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("url", redactServiceURL(n.gotifyURL)).
			Trace("Gotify API URL loaded")
	}

	// Parse the API URL.
	apiURL, err := url.Parse(n.gotifyURL)
	if err != nil {
		clog.WithError(err).Debug("Failed to parse Gotify URL")

		return "", fmt.Errorf("failed to generate Gotify URL: %w", err)
	}

	// Configure Gotify settings.
	config := &gotify.Config{
		Host:       apiURL.Host,
		Path:       apiURL.Path,
		DisableTLS: apiURL.Scheme == "http",
		Token:      n.gotifyAppToken,
	}

	urlStr := config.GetURL().String()

	clog.WithField("disable_tls", apiURL.Scheme == "http").
		Debug("Generated Gotify service URL")

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("service_url", redactServiceURL(urlStr)).
			Trace("Generated Gotify service URL")
	}

	return urlStr, nil
}

// GetEntries returns nil for legacy notifiers.
//
// Returns:
//   - []*logrus.Entry: Always nil.
//
// Deprecated: This method is part of the legacy gotify notifier and will be removed
// for the v2 release.
func (n *gotifyTypeNotifier) GetEntries() []*logrus.Entry {
	return nil
}

// SendFilteredEntries does nothing for legacy notifiers.
//
// Parameters:
//   - entries: Ignored.
//   - report: Ignored.
//
// Deprecated: This method is part of the legacy gotify notifier and will be removed
// for the v2 release.
func (n *gotifyTypeNotifier) SendFilteredEntries(_ []*logrus.Entry, _ types.Report) {
	// Legacy notifiers do not support filtered entries.
}
