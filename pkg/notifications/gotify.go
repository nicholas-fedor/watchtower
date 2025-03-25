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

	notifier := &gotifyTypeNotifier{
		gotifyURL:                apiURL,
		gotifyAppToken:           token,
		gotifyInsecureSkipVerify: skipVerify,
	}

	return notifier
}

// getGotifyToken retrieves and validates the Gotify token from flags.
// It exits with a fatal error if the token is empty.
func getGotifyToken(flags *pflag.FlagSet) string {
	gotifyToken, _ := flags.GetString("notification-gotify-token")
	if len(gotifyToken) < 1 {
		logrus.Fatal(
			"Required argument --notification-gotify-token(cli) or WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN(env) is empty.",
		)
	}

	return gotifyToken
}

// getGotifyURL retrieves and validates the Gotify URL from flags.
// It ensures the URL starts with "http://" or "https://", warning if insecure.
func getGotifyURL(flags *pflag.FlagSet) string {
	gotifyURL, _ := flags.GetString("notification-gotify-url")

	switch {
	case len(gotifyURL) < 1:
		logrus.Fatal(
			"Required argument --notification-gotify-url(cli) or WATCHTOWER_NOTIFICATION_GOTIFY_URL(env) is empty.",
		)
	case !strings.HasPrefix(gotifyURL, "http://") && !strings.HasPrefix(gotifyURL, "https://"):
		logrus.Fatal("Gotify URL must start with \"http://\" or \"https://\"")
	case strings.HasPrefix(gotifyURL, "http://"):
		logrus.Warn("Using an HTTP url for Gotify is insecure")
	}

	return gotifyURL
}

// GetURL generates the Gotify URL for the notifier based on its configuration.
// It parses the API URL and constructs the service URL with TLS settings.
func (n *gotifyTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	apiURL, err := url.Parse(n.gotifyURL)
	if err != nil {
		return "", fmt.Errorf("failed to generate Gotify URL: %w", err)
	}

	config := &gotify.Config{
		Host:       apiURL.Host,
		Path:       apiURL.Path,
		DisableTLS: apiURL.Scheme == "http",
		Token:      n.gotifyAppToken,
	}

	return config.GetURL().String(), nil
}
