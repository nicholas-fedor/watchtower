package notifications

import (
	"net/url"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/teams"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	msTeamsType = "msteams"
)

type msTeamsTypeNotifier struct {
	webHookURL string
	data       bool
}

func newMsTeamsNotifier(cmd *cobra.Command) types.ConvertibleNotifier {

	flags := cmd.Flags()

	webHookURL, _ := flags.GetString("notification-msteams-hook")
	if len(webHookURL) <= 0 {
		logrus.Fatal("Required argument --notification-msteams-hook(cli) or WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL(env) is empty.")
	}

	withData, _ := flags.GetBool("notification-msteams-data")
	n := &msTeamsTypeNotifier{
		webHookURL: webHookURL,
		data:       withData,
	}

	return n
}

func (n *msTeamsTypeNotifier) GetURL(c *cobra.Command) (string, error) {
	webhookURL, err := url.Parse(n.webHookURL)
	if err != nil {
		return "", err
	}

	config, err := teams.ConfigFromWebhookURL(*webhookURL)
	if err != nil {
		return "", err
	}

	config.Color = ColorHex

	return config.GetURL().String(), nil
}
