package notifications

import (
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// NewNotifier creates and returns a new Notifier, using global configuration.
func NewNotifier(c *cobra.Command) types.Notifier {
	flag := c.Flags()

	level, _ := flag.GetString("notifications-level")

	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logrus.Fatalf("Notifications invalid log level: %s", err.Error())
	}

	reportTemplate, _ := flag.GetBool("notification-report")
	stdout, _ := flag.GetBool("notification-log-stdout")
	tplString, _ := flag.GetString("notification-template")
	urls, _ := flag.GetStringArray("notification-url")

	data := GetTemplateData(c)
	urls, delay := AppendLegacyUrls(urls, c)

	return createNotifier(urls, logLevel, tplString, !reportTemplate, data, stdout, delay)
}

// AppendLegacyUrls creates shoutrrr equivalent URLs from legacy notification flags.
func AppendLegacyUrls(urls []string, cmd *cobra.Command) ([]string, time.Duration) {
	// Parse notification notificationTypes and create notifiers.
	notificationTypes, err := cmd.Flags().GetStringSlice("notifications")
	if err != nil {
		logrus.WithError(err).Fatal("could not read notifications argument")
	}

	legacyDelay := time.Duration(0)

	for _, notificationType := range notificationTypes {
		var legacyNotifier types.ConvertibleNotifier

		var err error

		switch notificationType {
		case emailType:
			legacyNotifier = newEmailNotifier(cmd)
		case slackType:
			legacyNotifier = newSlackNotifier(cmd)
		case msTeamsType:
			legacyNotifier = newMsTeamsNotifier(cmd)
		case gotifyType:
			legacyNotifier = newGotifyNotifier(cmd)
		case shoutrrrType:
			continue
		default:
			logrus.Fatalf("Unknown notification type %q", notificationType)
			// Not really needed, used for nil checking static analysis
			continue
		}

		shoutrrrURL, err := legacyNotifier.GetURL(cmd)
		if err != nil {
			logrus.Fatal("failed to create notification config: ", err)
		}

		urls = append(urls, shoutrrrURL)

		if delayNotifier, ok := legacyNotifier.(types.DelayNotifier); ok {
			legacyDelay = delayNotifier.GetDelay()
		}

		logrus.WithField("URL", shoutrrrURL).Trace("created Shoutrrr URL from legacy notifier")
	}

	delay := GetDelay(cmd, legacyDelay)

	return urls, delay
}

// GetDelay returns the legacy delay if defined, otherwise the delay as set by args is returned.
func GetDelay(c *cobra.Command, legacyDelay time.Duration) time.Duration {
	if legacyDelay > 0 {
		return legacyDelay
	}

	delay, _ := c.PersistentFlags().GetInt("notifications-delay")
	if delay > 0 {
		return time.Duration(delay) * time.Second
	}

	return time.Duration(0)
}

// GetTitle formats the title based on the passed hostname and tag.
func GetTitle(hostname string, tag string) string {
	titleBuilder := strings.Builder{}

	if tag != "" {
		titleBuilder.WriteRune('[')
		titleBuilder.WriteString(tag)
		titleBuilder.WriteRune(']')
		titleBuilder.WriteRune(' ')
	}

	titleBuilder.WriteString("Watchtower updates")

	if hostname != "" {
		titleBuilder.WriteString(" on ")
		titleBuilder.WriteString(hostname)
	}

	return titleBuilder.String()
}

// GetTemplateData populates the static notification data from flags and environment.
func GetTemplateData(c *cobra.Command) StaticData {
	flag := c.PersistentFlags()

	hostname, _ := flag.GetString("notifications-hostname")
	if hostname == "" {
		hostname, _ = os.Hostname()
	}

	title := ""

	if skip, _ := flag.GetBool("notification-skip-title"); !skip {
		tag, _ := flag.GetString("notification-title-tag")
		if tag == "" {
			// For legacy email support
			tag, _ = flag.GetString("notification-email-subjecttag")
		}

		title = GetTitle(hostname, tag)
	}

	return StaticData{
		Host:  hostname,
		Title: title,
	}
}

// ColorHex is the default notification color used for services that support it (formatted as a CSS hex string).
const ColorHex = "#406170"

// ColorInt is the default notification color used for services that support it (as an int value).
const ColorInt = 0x406170
