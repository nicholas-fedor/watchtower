package email

import (
	"errors"
	"fmt"
	"time"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/email/smtp"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
)

// Type is the identifier for email notifications.
//
// Deprecated: Legacy email notification type is deprecated.
// Use --notification-url with an smtp:// URL instead.
//
// TODO: Remove Type constant for the v2 release.
//
//nolint:godox
const Type = "email"

// defaultTimeout is the default duration for SMTP operations.
const defaultTimeout = 10 * time.Second

// Errors for email notification configuration.
var (
	// errInvalidPortRange indicates that the specified SMTP port is outside the valid range (0-65535).
	errInvalidPortRange = errors.New("port out of valid range (0-65535)")
	// errEmptyFrom indicates that no sender address is set.
	errEmptyFrom = errors.New("the notification-email-from configuration option is empty")
	// errEmptyTo indicates that no recipient address is set.
	errEmptyTo = errors.New("the notification-email-to configuration option is empty")
	// errEmptyServer indicates that no SMTP server is set.
	errEmptyServer = errors.New("the notification-email-server configuration option is empty")
)

// URL converts the legacy email settings to a Shoutrrr SMTP URL.
//
// Parameters:
//   - log: Logger for configuration diagnostics.
//   - cfg: Deprecated email SMTP settings (from process config or flags).
//
// Returns:
//   - string: SMTP URL.
//   - error: Non-nil when a required setting is empty: the from address, the
//     to address or the server. Also non-nil when the port is outside 0 to
//     65535.
//
// Deprecated: Legacy email notifier is deprecated.
// Use --notification-url with an smtp:// URL instead.
//
// TODO: Remove URL for the v2 release.
//
//nolint:godox
func URL(log *zerolog.Logger, cfg notifyConfig.Legacy) (string, error) {
	if cfg.EmailFrom == "" {
		return "", errEmptyFrom
	}

	if cfg.EmailTo == "" {
		return "", errEmptyTo
	}

	if cfg.EmailServer == "" {
		return "", errEmptyServer
	}

	if cfg.EmailPort < 0 || cfg.EmailPort > 65535 {
		return "", fmt.Errorf("port %d: %w", cfg.EmailPort, errInvalidPortRange)
	}

	clog := log.With().
		Str("from", cfg.EmailFrom).
		Str("to", cfg.EmailTo).
		Str("server", cfg.EmailServer).
		Int("port", cfg.EmailPort).
		Bool("tls_skip", cfg.EmailTLSSkipVerify).
		Logger()
	clog.Debug().Msg("Generating SMTP URL")

	// Log credential presence only at trace level. Never log the password value.
	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("user", cfg.EmailUser).
			Bool("credentials_configured", cfg.EmailUser != "" || cfg.EmailPassword != "").
			Msg("Email notifier credentials loaded")
	}

	conf := &smtp.Config{
		FromAddress:   cfg.EmailFrom,
		FromName:      "Watchtower",
		ToAddresses:   []string{cfg.EmailTo},
		Port:          uint16(cfg.EmailPort),
		Host:          cfg.EmailServer,
		Username:      cfg.EmailUser,
		Password:      cfg.EmailPassword,
		UseStartTLS:   true,
		UseHTML:       false,
		Encryption:    smtp.EncMethods.Auto,
		Auth:          smtp.AuthTypes.None,
		ClientHost:    "localhost",
		SkipTLSVerify: cfg.EmailTLSSkipVerify,
		Timeout:       defaultTimeout,
	}

	// Enable authentication if credentials provided.
	if cfg.EmailUser != "" {
		conf.Auth = smtp.AuthTypes.Plain
	}

	url := conf.GetURL().String()

	authEnabled := conf.Auth == smtp.AuthTypes.Plain

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Bool("auth_enabled", authEnabled).
			Str("url", redact.ServiceURL(url)).
			Msg("Generated SMTP URL")
	} else {
		clog.Debug().
			Bool("auth_enabled", authEnabled).
			Msg("Generated SMTP URL")
	}

	return url, nil
}
