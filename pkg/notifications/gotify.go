// Package notifications provides mechanisms for sending notifications via various services.
// This file implements Gotify notification functionality.
package notifications

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/gotify"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	gotifyType = "gotify"
)

// gotifyTypeNotifier handles Gotify notifications with URL, token, and TLS settings.
// It configures the Gotify service for sending messages.
type gotifyTypeNotifier struct {
	gotifyURL                string
	gotifyAppToken           string
	gotifyInsecureSkipVerify bool
}

// newGotifyNotifier creates a new Gotify notifier from command-line flags.
// It validates and retrieves the URL and token, setting TLS skip verification as needed.
func newGotifyNotifier(c *cobra.Command) types.ConvertibleNotifier {
	flags := c.Flags()

	apiURL := getGotifyURL(flags)
	token := getGotifyToken(flags)
	skipVerify, _ := flags.GetBool("notification-gotify-tls-skip-verify")

	clog := logrus.WithFields(logrus.Fields{
		"url":         apiURL,
		"skip_verify": skipVerify,
	})
	clog.Debug("Initializing Gotify notifier")

	// Log token at trace level for sensitivity
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("token", token).Trace("Gotify notifier token loaded")
	}

	return &gotifyTypeNotifier{
		gotifyURL:                apiURL,
		gotifyAppToken:           token,
		gotifyInsecureSkipVerify: skipVerify,
	}
}

// getGotifyToken retrieves and validates the Gotify token from flags.
// It returns the token or exits with a fatal error if empty.
func getGotifyToken(flags *pflag.FlagSet) string {
	gotifyToken, _ := flags.GetString("notification-gotify-token")
	clog := logrus.WithField("flag", "notification-gotify-token")

	if len(gotifyToken) < 1 {
		clog.Fatal(
			"Gotify token is empty; required argument --notification-gotify-token(cli) or WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN(env) is empty",
		)
	}

	clog.WithField("token_length", len(gotifyToken)).Debug("Retrieved Gotify token")

	return gotifyToken
}

// getGotifyURL retrieves and validates the Gotify URL from flags.
// It ensures the URL starts with "http://" or "https://", warning if insecure.
func getGotifyURL(flags *pflag.FlagSet) string {
	gotifyURL, _ := flags.GetString("notification-gotify-url")
	clog := logrus.WithFields(logrus.Fields{
		"flag": "notification-gotify-url",
		"url":  gotifyURL,
	})

	if len(gotifyURL) < 1 {
		clog.Fatal(
			"Gotify URL is empty; required argument --notification-gotify-url(cli) or WATCHTOWER_NOTIFICATION_GOTIFY_URL(env) is empty",
		)
	}

	if !strings.HasPrefix(gotifyURL, "http://") && !strings.HasPrefix(gotifyURL, "https://") {
		clog.Fatal("Gotify URL must start with \"http://\" or \"https://\"")
	}

	if strings.HasPrefix(gotifyURL, "http://") {
		clog.Warn("Using an HTTP URL for Gotify is insecure")
	}

	clog.WithField("scheme", strings.Split(gotifyURL, ":")[0]).Debug("Validated Gotify URL")

	return gotifyURL
}

// GetURL generates the Gotify URL for the notifier based on its configuration.
// It parses the API URL and constructs the service URL with TLS settings.
func (n *gotifyTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.WithField("url", n.gotifyURL)
	clog.Debug("Generating Gotify service URL")

	apiURL, err := url.Parse(n.gotifyURL)
	if err != nil {
		clog.WithError(err).Debug("Failed to parse Gotify URL")

		return "", fmt.Errorf("failed to generate Gotify URL: %w", err)
	}

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
