// Package notifications provides mechanisms for sending notifications via various services.
// This file implements Gotify notification functionality.
package notifications

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/push/gotify"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// gotifyType is the identifier for Gotify notifications.
const gotifyType = "gotify"

// gotifyTypeNotifier handles Gotify notifications.
//
// It configures URL, token, and TLS settings.
type gotifyTypeNotifier struct {
	gotifyURL                string // Gotify server URL.
	gotifyAppToken           string // Gotify application token.
	gotifyInsecureSkipVerify bool   // Skip TLS verification if true.
}

// newGotifyNotifier creates a Gotify notifier from command-line flags.
//
// Parameters:
//   - c: Cobra command with flags.
//
// Returns:
//   - types.ConvertibleNotifier: New Gotify notifier instance.
func newGotifyNotifier(c *cobra.Command) types.ConvertibleNotifier {
	flags := c.Flags()

	// Extract and validate configuration.
	apiURL := getGotifyURL(flags)
	token := getGotifyToken(flags)
	skipVerify, _ := flags.GetBool("notification-gotify-tls-skip-verify")

	clog := logrus.WithFields(logrus.Fields{
		"url":         apiURL,
		"skip_verify": skipVerify,
	})
	clog.Debug("Initializing Gotify notifier")

	// Log token only at trace level for security.
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("token", token).Trace("Gotify notifier token loaded")
	}

	return &gotifyTypeNotifier{
		gotifyURL:                apiURL,
		gotifyAppToken:           token,
		gotifyInsecureSkipVerify: skipVerify,
	}
}

// getGotifyToken retrieves the Gotify token from flags.
//
// Parameters:
//   - flags: Flag set to check.
//
// Returns:
//   - string: Token value (fatal if empty).
func getGotifyToken(flags *pflag.FlagSet) string {
	gotifyToken, _ := flags.GetString("notification-gotify-token")
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

// getGotifyURL retrieves and validates the Gotify URL from flags.
//
// Parameters:
//   - flags: Flag set to check.
//
// Returns:
//   - string: Validated URL (fatal if empty or malformed).
func getGotifyURL(flags *pflag.FlagSet) string {
	gotifyURL, _ := flags.GetString("notification-gotify-url")
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

// GetURL generates the Gotify service URL from the notifier’s configuration.
//
// Parameters:
//   - c: Cobra command (unused here).
//
// Returns:
//   - string: Gotify service URL.
//   - error: Non-nil if URL parsing fails, nil on success.
func (n *gotifyTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.WithField("url", n.gotifyURL)
	clog.Debug("Generating Gotify service URL")

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
	clog.WithFields(logrus.Fields{
		"service_url": urlStr,
		"disable_tls": apiURL.Scheme == "http",
	}).Debug("Generated Gotify service URL")

	return urlStr, nil
}
