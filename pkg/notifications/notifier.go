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

// NewNotifier creates a new Notifier from global configuration.
//
// Parameters:
//   - c: Cobra command with flags.
//
// Returns:
//   - types.Notifier: Configured notifier instance.
func NewNotifier(c *cobra.Command) types.Notifier {
	flag := c.Flags()

	// Parse log level from flags.
	level, _ := flag.GetString("notifications-level")
	clog := logrus.WithField("level", level)
	clog.Debug("Parsing notifications log level")

	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		clog.WithError(err).Fatal("Invalid notifications log level")
	}

	// Extract notification settings.
	reportTemplate, _ := flag.GetBool("notification-report")
	stdout, _ := flag.GetBool("notification-log-stdout")
	tplString, _ := flag.GetString("notification-template")
	urls, _ := flag.GetStringArray("notification-url")

	data := GetTemplateData(c)
	urls, delay := AppendLegacyUrls(urls, c)

	// Use report template when enabled, otherwise use legacy template.
	legacy := !reportTemplate

	clog.WithFields(logrus.Fields{
		"urls":        urls,
		"template":    tplString,
		"skip_report": !reportTemplate,
		"stdout":      stdout,
		"delay":       delay,
		"hostname":    data.Host,
		"title":       data.Title,
		"legacy":      legacy,
	}).Debug("Creating notifier with configuration")

	return createNotifier(urls, logLevel, tplString, legacy, data, stdout, delay)
}

// AppendLegacyUrls adds shoutrrr URLs from legacy notification flags.
//
// Parameters:
//   - urls: Initial URL list.
//   - cmd: Cobra command with flags.
//
// Returns:
//   - []string: Updated URL list.
//   - time.Duration: Notification delay.
func AppendLegacyUrls(urls []string, cmd *cobra.Command) ([]string, time.Duration) {
	clog := logrus.WithField("function", "AppendLegacyUrls")
	clog.Debug("Appending legacy notification URLs")

	// Fetch legacy notification types.
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

		// Generate shoutrrr URL from legacy notifier.
		shoutrrrURL, err := legacyNotifier.GetURL(cmd)
		if err != nil {
			clog.WithError(err).
				WithField("type", notificationType).
				Fatal("Failed to create notification config")
		}

		urls = append(urls, shoutrrrURL)

		// Check for delay if supported.
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

// GetDelay determines the notification delay from flags or legacy value.
//
// Parameters:
//   - c: Cobra command with flags.
//   - legacyDelay: Delay from legacy notifier.
//
// Returns:
//   - time.Duration: Selected delay.
func GetDelay(c *cobra.Command, legacyDelay time.Duration) time.Duration {
	clog := logrus.WithField("legacy_delay", legacyDelay)
	clog.Debug("Determining notification delay")

	// Use legacy delay if set.
	if legacyDelay > 0 {
		clog.Debug("Using legacy delay")

		return legacyDelay
	}

	// Check configured delay from flags.
	delay, _ := c.PersistentFlags().GetInt("notifications-delay")
	if delay > 0 {
		delayDuration := time.Duration(delay) * time.Second
		clog.WithField("delay", delayDuration).Debug("Using configured delay from flags")

		return delayDuration
	}

	clog.Debug("No delay configured, using zero")

	return time.Duration(0)
}

// GetTitle formats the notification title with hostname and tag.
//
// Parameters:
//   - hostname: Hostname to include.
//   - tag: Optional tag prefix.
//
// Returns:
//   - string: Formatted title.
func GetTitle(hostname string, tag string) string {
	clog := logrus.WithFields(logrus.Fields{
		"hostname": hostname,
		"tag":      tag,
	})
	clog.Debug("Generating notification title")

	// Build title with optional tag and hostname.
	b := strings.Builder{}
	if tag != "" {
		b.WriteRune('[')
		b.WriteString(tag)
		b.WriteRune(']')
		b.WriteRune(' ')
	}

	b.WriteString("Watchtower updates")

	if hostname != "" {
		b.WriteString(" on ")
		b.WriteString(hostname)
	}

	title := b.String()
	clog.WithField("title", title).Debug("Generated notification title")

	return title
}

// GetTemplateData populates static notification data from flags and env.
//
// Parameters:
//   - c: Cobra command with flags.
//
// Returns:
//   - StaticData: Populated data.
func GetTemplateData(c *cobra.Command) StaticData {
	flag := c.PersistentFlags()

	// Get hostname from flag or system.
	hostname, _ := flag.GetString("notifications-hostname")
	clog := logrus.WithField("hostname_flag", hostname)
	clog.Debug("Retrieving template data")

	if hostname == "" {
		hostname, _ = os.Hostname()
		clog.WithField("hostname", hostname).Debug("Using system hostname")
	}

	// Generate title unless skipped.
	title := ""

	if skip, _ := flag.GetBool("notification-skip-title"); !skip {
		tag, _ := flag.GetString("notification-title-tag")
		if tag == "" {
			// Check legacy email tag.
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
