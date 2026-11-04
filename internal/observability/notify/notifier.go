package notify

import (
	"errors"
	"fmt"
	"iter"
	"os"
	"strings"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/rs/zerolog"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/format"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/legacy/email"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/legacy/gotify"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/legacy/msteams"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/legacy/slack"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// legacyURL is a Shoutrrr URL converted from a legacy notification type.
type legacyURL struct {
	notificationType string // Legacy notification type the URL came from.
	url              string // Converted Shoutrrr URL.
	//nolint:staticcheck // SA1019: legacy ConvertibleNotifier until v2.
	notifier types.ConvertibleNotifier // Legacy notifier that built the URL.
}

// errUnknownNotificationType indicates an unsupported legacy notification type name.
var errUnknownNotificationType = errors.New("unknown notification type")

// NewNotifier constructs the notification client from resolved process settings.
//
// It parses the notification log level, loads an optional template file, builds static
// template data, appends legacy Shoutrrr URLs when configured, and creates the client.
//
// Parameters:
//   - log: Process logger for configuration-time diagnostics (required and non-nil).
//   - cfg: Notification settings from settings.Load (Config.Notify).
//
// Returns:
//   - types.Notifier: Configured notifier instance.
func NewNotifier(log *zerolog.Logger, cfg notifyConfig.Notify) types.Notifier {
	if log == nil {
		panic("notify: log is required")
	}

	// Parse the notification log level from resolved configuration.
	// Avoid the field name level. Zerolog reserves it for event severity.
	clog := log.With().Str("notifications_level", cfg.Level).Logger()
	clog.Debug().Msg("Parsing notifications log level")

	logLevel, err := logging.ParseLevel(cfg.Level)
	if err != nil {
		clog.Fatal().Err(err).Msg("Invalid notifications log level")
	}

	// Prefer template file contents when set. Otherwise use the inline template string.
	tplString := cfg.Template

	if cfg.TemplateFile != "" {
		content, readErr := os.ReadFile(cfg.TemplateFile)
		if readErr != nil {
			clog.Fatal().
				Err(readErr).
				Str("file", cfg.TemplateFile).
				Msg("Failed to read notification template file")
		}

		tplString = string(content)

		clog.Debug().
			Str("file", cfg.TemplateFile).
			Msg("Loaded notification template from file")
	}

	data := templateData(log, cfg.Hostname, cfg.TitleTag, cfg.EmailSubjectTag, cfg.SkipTitle)

	// Start from configured Shoutrrr URLs, then append any legacy type URLs.
	urls := append([]string(nil), cfg.URLs...)
	legacyDelay := time.Duration(0)

	for legacy, err := range convertLegacyNotifiers(log, cfg.LegacyTypes, cfg.Legacy) {
		if err != nil {
			msg := "Failed to create notification config"
			if errors.Is(err, errUnknownNotificationType) {
				msg = "Unknown notification type"
			}

			clog.Fatal().Err(err).Msg(msg)
		}

		urls = append(urls, legacy.url)

		// A legacy notifier with its own delay sets the notification delay.
		//nolint:staticcheck // SA1019: legacy DelayNotifier until v2.
		delayNotifier, ok := legacy.notifier.(types.DelayNotifier)
		if ok {
			legacyDelay = delayNotifier.GetDelay()
			clog.Debug().
				Str("type", legacy.notificationType).
				Dur("delay", legacyDelay).
				Msg("Retrieved delay from legacy notifier")
		}
	}

	delay := getDelay(log, cfg.DelaySeconds, legacyDelay)

	// Use report template when enabled, otherwise use legacy template.
	legacyTemplate := !cfg.Report

	templateSource := "inline"
	if cfg.TemplateFile != "" {
		templateSource = cfg.TemplateFile
	}

	clog.Debug().
		Int("url_count", len(urls)).
		Str("template_source", templateSource).
		Int("template_bytes", len(tplString)).
		Bool("skip_report", !cfg.Report).
		Bool("stdout", cfg.LogStdout).
		Dur("delay", delay).
		Str("hostname", data.Host).
		Str("title", data.Title).
		Bool("legacy", legacyTemplate).
		Msg("Creating notifier with configuration")

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Strs("urls", redact.ServiceURLs(urls)).
			Msg("Notifier Shoutrrr URLs loaded")
	}

	return createNotifier(
		log,
		urls,
		logLevel,
		tplString,
		legacyTemplate,
		data,
		cfg.LogStdout,
		delay,
	)
}

// BuildURLs builds Shoutrrr notification URLs from resolved notification settings
// without initializing a notifier.
//
// It returns configured URLs plus any legacy Shoutrrr URLs generated from deprecated
// notification types. Shoutrrr reads every URL as it does when Watchtower starts.
// A URL it rejects fails the call. Conversion and URL errors are returned. A
// legacy notifier whose required setting is empty still exits the process
// itself, such as for an empty Slack hook URL.
//
// Parameters:
//   - log: Logger for legacy notifier construction diagnostics and Shoutrrr's
//     warnings about the URLs.
//   - cfg: Notification settings from settings.Load (Config.Notify).
//
// Returns:
//   - []string: Shoutrrr URLs ready for output or notification use.
//   - error: Non-nil when Shoutrrr rejects a URL. Also non-nil when a legacy
//     notification type is unknown or its notifier cannot generate a URL.
//
// TODO: Remove BuildURLs after the v2 release.
//
//nolint:godox
func BuildURLs(log *zerolog.Logger, cfg notifyConfig.Notify) ([]string, error) {
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}

	// Adding a URL to the sender reads it and logs Shoutrrr's warnings, such as
	// a deprecated key. It opens no connection. Services connect on their
	// first send.
	sender, err := shoutrrr.NewSenderWithOptions(
		newShoutrrrLogger(log, log, shoutrrrLogPrefix),
		shoutrrrTypes.SenderOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("create notification sender: %w", err)
	}

	for i, rawURL := range cfg.URLs {
		err = sender.AddService(rawURL)
		if err != nil {
			return nil, fmt.Errorf("notification URL %d: %w", i+1, err)
		}
	}

	urls := append([]string(nil), cfg.URLs...)

	// Each converted URL is checked before the next legacy notifier is built.
	for legacy, err := range convertLegacyNotifiers(log, cfg.LegacyTypes, cfg.Legacy) {
		if err != nil {
			return nil, err
		}

		err = sender.AddService(legacy.url)
		if err != nil {
			return nil, fmt.Errorf("check %q notification URL: %w", legacy.notificationType, err)
		}

		urls = append(urls, legacy.url)
	}

	return urls, nil
}

// templateData builds static notification title/host data from resolved values.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - hostname: Hostname from configuration, or empty to use the system hostname.
//   - titleTag: Optional title prefix tag.
//   - emailSubjectTag: Deprecated fallback tag when titleTag is empty.
//   - skipTitle: When true, title is left empty.
//
// Returns:
//   - format.StaticData: Host and title for notification templates.
func templateData(log *zerolog.Logger, hostname, titleTag, emailSubjectTag string, skipTitle bool) format.StaticData {
	clog := log.With().Str("hostname_flag", hostname).Logger()
	clog.Debug().Msg("Retrieving template data")

	// Get hostname from configuration or system.
	if hostname == "" {
		hostname, _ = os.Hostname()
		clog.Debug().Str("hostname", hostname).Msg("Using system hostname")
	}

	title := ""

	if !skipTitle {
		tag := titleTag
		if tag == "" {
			tag = emailSubjectTag
			if tag != "" {
				clog.Warn().
					Str("tag", tag).
					Msg("Using deprecated email subject tag flag. Use the notification-title-tag configuration option instead.")
			}
		}

		title = getTitle(log, hostname, tag)
	}

	clog.Debug().
		Str("hostname", hostname).
		Str("title", title).
		Msg("Populated template data")

	return format.StaticData{
		Host:  hostname,
		Title: title,
	}
}

// legacyNotifierCtor builds a ConvertibleNotifier from resolved legacy settings.
//
//nolint:staticcheck // SA1019: legacy ConvertibleNotifier until v2.
type legacyNotifierCtor func(log *zerolog.Logger, legacy notifyConfig.Legacy) types.ConvertibleNotifier

// legacyNotifierCtors maps legacy notification type names to constructors.
// The shoutrrr type has no entry. The conversion skips it before the lookup.
//
//nolint:staticcheck // SA1019: legacy notification types until v2.
var legacyNotifierCtors = map[string]legacyNotifierCtor{
	email.Type:   email.New,
	slack.Type:   slack.New,
	msteams.Type: msteams.New,
	gotify.Type:  gotify.New,
}

// convertLegacyNotifiers converts legacy notification types to Shoutrrr URLs,
// one type at a time and in order. Each legacy notifier is built only when the
// caller asks for the next URL. A caller that stops at a URL or an error builds
// no later notifier.
//
// Parameters:
//   - log: Logger for diagnostics and the legacy notifiers' own checks.
//   - notificationTypes: Legacy type names (email, slack, msteams, gotify). The
//     shoutrrr type is skipped.
//   - legacy: Per-type settings for deprecated notifiers.
//
// Returns:
//   - iter.Seq2[legacyURL, error]: Each converted URL with its type and
//     notifier. An unknown type yields an error that names the type and ends
//     the sequence. So does a notifier that cannot generate its URL.
//
// Deprecated: Legacy notification types are deprecated.
// Use --notification-url instead.
//
// TODO: Remove convertLegacyNotifiers for the v2 release.
//
//nolint:godox
func convertLegacyNotifiers(
	log *zerolog.Logger,
	notificationTypes []string,
	legacy notifyConfig.Legacy,
) iter.Seq2[legacyURL, error] {
	return func(yield func(legacyURL, error) bool) {
		clog := log.With().Str("function", "convertLegacyNotifiers").Logger()
		clog.Debug().
			Strs("types", notificationTypes).
			Msg("Processing legacy notification types")

		for _, notificationType := range notificationTypes {
			if notificationType == shoutrrrType {
				continue
			}

			ctor, ok := legacyNotifierCtors[notificationType]
			if !ok {
				yield(legacyURL{}, fmt.Errorf("%w: %q", errUnknownNotificationType, notificationType))

				return
			}

			legacyNotifier := ctor(log, legacy)

			shoutrrrURL, err := legacyNotifier.GetURL()
			if err != nil {
				yield(legacyURL{}, fmt.Errorf("create %q notification config: %w", notificationType, err))

				return
			}

			clog.Trace().
				Str("type", notificationType).
				Str("url", redact.ServiceURL(shoutrrrURL)).
				Msg("Created Shoutrrr URL from legacy notifier")

			if !yield(legacyURL{notificationType: notificationType, url: shoutrrrURL, notifier: legacyNotifier}, nil) {
				return
			}
		}
	}
}

// getDelay selects the notification delay from a legacy value or configured seconds.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - delaySeconds: Configured delay in seconds (from Config.Notify.DelaySeconds).
//   - legacyDelay: Delay from a legacy notifier type, preferred when non-zero.
//
// Returns:
//   - time.Duration: Selected delay (legacy delay if set, otherwise delaySeconds, otherwise zero).
//
// Deprecated: Prefer NewNotifier with Config.Notify from settings.Load.
//
// TODO: Simplify getDelay to only use configured delay seconds when legacy types are removed.
//
//nolint:godox
func getDelay(log *zerolog.Logger, delaySeconds int, legacyDelay time.Duration) time.Duration {
	clog := log.With().Dur("legacy_delay", legacyDelay).Logger()
	clog.Debug().Msg("Determining notification delay")

	// Use legacy delay if set.
	if legacyDelay > 0 {
		clog.Debug().Msg("Using legacy delay")

		return legacyDelay
	}

	// Use configured delay when no legacy delay applies.
	if delaySeconds > 0 {
		delayDuration := time.Duration(delaySeconds) * time.Second
		clog.Debug().
			Dur("delay", delayDuration).
			Msg("Using configured delay")

		return delayDuration
	}

	clog.Debug().Msg("No delay configured, using zero")

	return 0
}

// getTitle formats the notification title with hostname and tag.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - hostname: Hostname to include.
//   - tag: Optional tag prefix.
//
// Returns:
//   - string: Formatted title.
func getTitle(log *zerolog.Logger, hostname, tag string) string {
	clog := log.With().
		Str("hostname", hostname).
		Str("tag", tag).
		Logger()
	clog.Debug().Msg("Generating notification title")

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
	clog.Debug().
		Str("title", title).
		Msg("Generated notification title")

	return title
}

// LogLegacyDeprecationWarnings logs deprecation warnings for legacy notification types.
//
// It iterates over the provided notification types and logs a warning for each
// legacy type, advising users to migrate to the notification-url configuration option.
//
// Parameters:
//   - log: Logger for warnings.
//   - notificationTypes: List of notification type strings to check.
func LogLegacyDeprecationWarnings(log *zerolog.Logger, notificationTypes []string) {
	for _, notificationType := range notificationTypes {
		switch notificationType {
		case email.Type, slack.Type, msteams.Type, gotify.Type: //nolint:staticcheck // SA1019: legacy notification types until v2.
			log.Warn().
				Msgf("Using deprecated legacy %s notification configuration. "+
					"Use the notification-url configuration option instead.",
					notificationType)
		}
	}
}
