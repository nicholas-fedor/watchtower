// Package notifications provides mechanisms for sending notifications via various services.
// This file implements the core notifier creation and configuration logic.
package notifications

import (
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ColorHex is the default notification color used for services that support it (formatted as a CSS hex string).
const ColorHex = "#406170"

// ColorInt is the default notification color used for services that support it (as an int value).
const ColorInt = 0x406170

// NewNotifier creates and returns a new Notifier, using global configuration.
func NewNotifier(c *cobra.Command) types.Notifier {
	flag := c.Flags()

	level, _ := flag.GetString("notifications-level")
	clog := logrus.WithField("level", level)
	clog.Debug("Parsing notifications log level")

	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		clog.WithError(err).Fatal("Invalid notifications log level")
	}

	reportTemplate, _ := flag.GetBool("notification-report")
	stdout, _ := flag.GetBool("notification-log-stdout")
	tplString, _ := flag.GetString("notification-template")
	urls, _ := flag.GetStringArray("notification-url")

	data := GetTemplateData(c)
	urls, delay := AppendLegacyUrls(urls, c)

	clog.WithFields(logrus.Fields{
		"urls":        urls,
		"template":    tplString,
		"skip_report": !reportTemplate,
		"stdout":      stdout,
		"delay":       delay,
		"hostname":    data.Host,
		"title":       data.Title,
	}).Debug("Creating notifier with configuration")

	return createNotifier(urls, logLevel, tplString, !reportTemplate, data, stdout, delay)
}

// AppendLegacyUrls creates shoutrrr equivalent URLs from legacy notification flags.
func AppendLegacyUrls(urls []string, cmd *cobra.Command) ([]string, time.Duration) {
	clog := logrus.WithField("function", "AppendLegacyUrls")
	clog.Debug("Appending legacy notification URLs")

	notificationTypes, err := cmd.Flags().GetStringSlice("notifications")
	if err != nil {
		clog.WithError(err).Fatal("Could not read notifications argument")
	}

	clog.WithField("types", notificationTypes).Debug("Processing legacy notification types")

	legacyDelay := time.Duration(0)

	for _, notificationType := range notificationTypes {
		var legacyNotifier types.ConvertibleNotifier

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
			clog.WithField("type", notificationType).Fatal("Unknown notification type")

			continue
		}

		shoutrrrURL, err := legacyNotifier.GetURL(cmd)
		if err != nil {
			clog.WithError(err).
				WithField("type", notificationType).
				Fatal("Failed to create notification config")
		}

		urls = append(urls, shoutrrrURL)

		if delayNotifier, ok := legacyNotifier.(types.DelayNotifier); ok {
			legacyDelay = delayNotifier.GetDelay()
			clog.WithFields(logrus.Fields{
				"type":  notificationType,
				"delay": legacyDelay,
			}).Debug("Retrieved delay from legacy notifier")
		}

		clog.WithFields(logrus.Fields{
			"type": notificationType,
			"url":  shoutrrrURL,
		}).Trace("Created Shoutrrr URL from legacy notifier")
	}

	delay := GetDelay(cmd, legacyDelay)
	clog.WithFields(logrus.Fields{
		"urls":  urls,
		"delay": delay,
	}).Debug("Completed legacy URL appending")

	return urls, delay
}

// GetDelay returns the legacy delay if defined, otherwise the delay as set by args is returned.
func GetDelay(c *cobra.Command, legacyDelay time.Duration) time.Duration {
	clog := logrus.WithField("legacy_delay", legacyDelay)
	clog.Debug("Determining notification delay")

	if legacyDelay > 0 {
		clog.Debug("Using legacy delay")

		return legacyDelay
	}

	delay, _ := c.PersistentFlags().GetInt("notifications-delay")
	if delay > 0 {
		delayDuration := time.Duration(delay) * time.Second
		clog.WithField("delay", delayDuration).Debug("Using configured delay from flags")

		return delayDuration
	}

	clog.Debug("No delay configured, using zero")

	return time.Duration(0)
}

// GetTitle formats the title based on the passed hostname and tag.
func GetTitle(hostname string, tag string) string {
	clog := logrus.WithFields(logrus.Fields{
		"hostname": hostname,
		"tag":      tag,
	})
	clog.Debug("Generating notification title")

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

	title := titleBuilder.String()
	clog.WithField("title", title).Debug("Generated notification title")

	return title
}

// GetTemplateData populates the static notification data from flags and environment.
func GetTemplateData(c *cobra.Command) StaticData {
	flag := c.PersistentFlags()

	hostname, _ := flag.GetString("notifications-hostname")
	clog := logrus.WithField("hostname_flag", hostname)
	clog.Debug("Retrieving template data")

	if hostname == "" {
		hostname, _ = os.Hostname()
		clog.WithField("hostname", hostname).Debug("Using system hostname")
	}

	title := ""

	if skip, _ := flag.GetBool("notification-skip-title"); !skip {
		tag, _ := flag.GetString("notification-title-tag")
		if tag == "" {
			// For legacy email support
			tag, _ = flag.GetString("notification-email-subjecttag")
			clog.WithField("tag", tag).Debug("Using legacy email subject tag")
		}

		title = GetTitle(hostname, tag)
	}

	clog.WithFields(logrus.Fields{
		"hostname": hostname,
		"title":    title,
	}).Debug("Populated template data")

	return StaticData{
		Host:  hostname,
		Title: title,
	}
}
