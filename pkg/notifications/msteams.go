package notifications

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/chat/teams"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	notifyConfig "github.com/nicholas-fedor/watchtower/internal/config/notify"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// msTeamsType is the identifier for Microsoft Teams notifications.
//
// Deprecated: Legacy msteams notification type is deprecated.
// Use --notification-url with a teams:// URL instead.
//
// TODO: Remove msTeamsType constant for the v2 release.
//
//nolint:godox
const msTeamsType = "msteams"

// Errors for Microsoft Teams notification configuration.
var (
	// errParseWebhookFailed indicates a failure to parse the Microsoft Teams webhook URL.
	errParseWebhookFailed = errors.New("failed to parse Microsoft Teams webhook URL")
)

// msTeamsTypeNotifier handles Microsoft Teams notifications via webhook.
//
// Deprecated: Legacy msteams notifier is deprecated.
// Use --notification-url with a teams:// URL instead.
//
// TODO: Remove msTeamsTypeNotifier for the v2 release.
//
//nolint:godox
type msTeamsTypeNotifier struct {
	webHookURL string
}

// newMsTeamsNotifier creates a Teams notifier from resolved legacy settings.
//
// Parameters:
//   - legacy: Deprecated MSTeams webhook settings (from process config or flags).
//
// Returns:
//   - types.ConvertibleNotifier: New Teams notifier instance.
//
// Deprecated: Legacy msteams notifier is deprecated.
// Use --notification-url with a teams:// URL instead.
//
// TODO: Remove newMsTeamsNotifier for the v2 release.
//
//nolint:godox
func newMsTeamsNotifier(legacy notifyConfig.Legacy) types.ConvertibleNotifier {
	webHookURL := legacy.MSTeamsHook
	clog := logrus.WithField("url", redactServiceURL(webHookURL))

	if len(webHookURL) == 0 {
		clog.Fatal(
			"Microsoft Teams webhook URL is empty; required argument --notification-msteams-hook(cli) or WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL(env) missing",
		)
	}

	return &msTeamsTypeNotifier{webHookURL: webHookURL}
}

// GetURL generates the Teams service URL from the notifier's webhook.
//
// Parameters:
//   - c: Cobra command (unused here).
//
// Returns:
//   - string: Teams service URL.
//   - error: Non-nil if parsing fails, nil on success.
//
// Deprecated: This method is part of the legacy msteams notifier and will be removed
// for the v2 release. Use --notification-url with a teams:// URL instead.
func (n *msTeamsTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.NewEntry(logrus.StandardLogger())
	clog.Debug("Generating Microsoft Teams service URL")

	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("url", redactServiceURL(n.webHookURL)).
			Trace("Microsoft Teams webhook URL loaded")
	}

	// Validate the webhook URL is parseable and absolute.
	parsed, err := url.Parse(n.webHookURL)
	if err != nil {
		clog.WithError(err).Debug("Failed to parse Microsoft Teams webhook URL")

		return "", fmt.Errorf("%w: %w", errParseWebhookFailed, err)
	}

	if parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("%w: expected https URL", errParseWebhookFailed)
	}

	// Create Teams config with the full webhook URL as the host.
	config := &teams.Config{
		Host:  n.webHookURL,
		Color: ColorHex,
	}

	urlStr := config.GetURL().String()
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithField("service_url", redactServiceURL(urlStr)).
			Trace("Generated Microsoft Teams service URL")
	} else {
		clog.Debug("Generated Microsoft Teams service URL")
	}

	return urlStr, nil
}

// GetEntries returns nil for legacy notifiers.
//
// Returns:
//   - []*logrus.Entry: Always nil.
//
// Deprecated: This method is part of the legacy msteams notifier and will be removed
// for the v2 release.
func (n *msTeamsTypeNotifier) GetEntries() []*logrus.Entry {
	return nil
}

// SendFilteredEntries does nothing for legacy notifiers.
//
// Parameters:
//   - entries: Ignored.
//   - report: Ignored.
//
// Deprecated: This method is part of the legacy msteams notifier and will be removed
// for the v2 release.
func (n *msTeamsTypeNotifier) SendFilteredEntries(_ []*logrus.Entry, _ types.Report) {
	// Legacy notifiers do not support filtered entries.
}
