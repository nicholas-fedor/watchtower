package notifications

import (
	"net/url"
	"strings"

	"github.com/containrrr/shoutrrr/pkg/services/gotify"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	gotifyType = "gotify"
)

type gotifyTypeNotifier struct {
	gotifyURL                string
	gotifyAppToken           string
	gotifyInsecureSkipVerify bool
}

func newGotifyNotifier(c *cobra.Command) types.ConvertibleNotifier {
	flags := c.Flags()

	apiURL := getGotifyURL(flags)
	token := getGotifyToken(flags)

	skipVerify, _ := flags.GetBool("notification-gotify-tls-skip-verify")

	n := &gotifyTypeNotifier{
		gotifyURL:                apiURL,
		gotifyAppToken:           token,
		gotifyInsecureSkipVerify: skipVerify,
	}

	return n
}

func getGotifyToken(flags *pflag.FlagSet) string {
	gotifyToken, _ := flags.GetString("notification-gotify-token")
	if len(gotifyToken) < 1 {
		logrus.Fatal("Required argument --notification-gotify-token(cli) or WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN(env) is empty.")
	}
	return gotifyToken
}

func getGotifyURL(flags *pflag.FlagSet) string {
	gotifyURL, _ := flags.GetString("notification-gotify-url")

	if len(gotifyURL) < 1 {
		logrus.Fatal("Required argument --notification-gotify-url(cli) or WATCHTOWER_NOTIFICATION_GOTIFY_URL(env) is empty.")
	} else if !(strings.HasPrefix(gotifyURL, "http://") || strings.HasPrefix(gotifyURL, "https://")) {
		logrus.Fatal("Gotify URL must start with \"http://\" or \"https://\"")
	} else if strings.HasPrefix(gotifyURL, "http://") {
		logrus.Warn("Using an HTTP url for Gotify is insecure")
	}

	return gotifyURL
}

func (n *gotifyTypeNotifier) GetURL(c *cobra.Command) (string, error) {
	apiURL, err := url.Parse(n.gotifyURL)
	if err != nil {
		return "", err
	}

	config := &gotify.Config{
		Host:       apiURL.Host,
		Path:       apiURL.Path,
		DisableTLS: apiURL.Scheme == "http",
		Token:      n.gotifyAppToken,
	}

	return config.GetURL().String(), nil
}
