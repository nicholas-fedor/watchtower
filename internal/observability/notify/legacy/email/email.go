package email

import (
	"errors"
	"fmt"
	"time"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/email/smtp"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	notifyConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/notify"
	"github.com/nicholas-fedor/watchtower/pkg/types"
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

// emailTypeNotifier handles email notifications via SMTP.
//
// Deprecated: Legacy email notifier is deprecated.
// Use --notification-url with an smtp:// URL instead.
//
// TODO: Remove emailTypeNotifier for the v2 release.
//
//nolint:godox
type emailTypeNotifier struct {
	log                    *zerolog.Logger
	From, To               string // Sender and recipient email addresses.
	Server, User, Password string // SMTP server details.
	Port                   int    // SMTP server port.
	tlsSkipVerify          bool   // Skip TLS verification if true.
}

// New creates an email notifier from resolved legacy settings.
//
// Parameters:
//   - log: Logger for configuration diagnostics.
//   - cfg: Deprecated email SMTP settings (from process config or flags).
//
// Returns:
//   - types.ConvertibleNotifier: New email notifier instance.
//   - error: Non-nil when a required setting is empty: the from address, the
//     to address or the server.
//
// Deprecated: Legacy email notifier is deprecated.
// Use --notification-url with an smtp:// URL instead.
//
// TODO: Remove New for the v2 release.
//
//nolint:godox
func New(log *zerolog.Logger, cfg notifyConfig.Legacy) (types.ConvertibleNotifier, error) {
	if cfg.EmailFrom == "" {
		return nil, errEmptyFrom
	}

	if cfg.EmailTo == "" {
		return nil, errEmptyTo
	}

	if cfg.EmailServer == "" {
		return nil, errEmptyServer
	}

	from := cfg.EmailFrom
	to := cfg.EmailTo //nolint:varnamelen
	server := cfg.EmailServer
	user := cfg.EmailUser
	password := cfg.EmailPassword
	port := cfg.EmailPort
	tlsSkipVerify := cfg.EmailTLSSkipVerify

	clog := log.With().
		Str("from", from).
		Str("to", to).
		Str("server", server).
		Int("port", port).
		Bool("tls_skip", tlsSkipVerify).
		Logger()
	clog.Debug().Msg("Initializing email notifier from flags")

	// Log credential presence only at trace level. Never log the password value.
	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("user", user).
			Bool("credentials_configured", user != "" || password != "").
			Msg("Email notifier credentials loaded")
	}

	return &emailTypeNotifier{
		From:          from,
		To:            to,
		Server:        server,
		User:          user,
		Password:      password,
		Port:          port,
		tlsSkipVerify: tlsSkipVerify,
		log:           log,
	}, nil
}

// GetURL generates the SMTP URL from the notifier's configuration.
//
// Returns:
//   - string: SMTP URL.
//   - error: Non-nil if port is invalid, nil on success.
//
// Deprecated: This method is part of the legacy email notifier and will be removed
// for the v2 release. Use --notification-url with an smtp:// URL instead.
func (e *emailTypeNotifier) GetURL() (string, error) {
	clog := e.log.With().
		Str("from", e.From).
		Str("to", e.To).
		Str("server", e.Server).
		Int("port", e.Port).
		Logger()
	clog.Debug().Msg("Generating SMTP URL")

	// Validate port range (0-65535).
	if e.Port < 0 || e.Port > 65535 {
		clog.Debug().Int("port", e.Port).Msg("Invalid SMTP port")

		return "", fmt.Errorf("port %d: %w", e.Port, errInvalidPortRange)
	}

	// Configure SMTP settings.
	port := uint16(e.Port)

	conf := &smtp.Config{
		FromAddress:   e.From,
		FromName:      "Watchtower",
		ToAddresses:   []string{e.To},
		Port:          port,
		Host:          e.Server,
		Username:      e.User,
		Password:      e.Password,
		UseStartTLS:   true,
		UseHTML:       false,
		Encryption:    smtp.EncMethods.Auto,
		Auth:          smtp.AuthTypes.None,
		ClientHost:    "localhost",
		SkipTLSVerify: e.tlsSkipVerify,
		Timeout:       defaultTimeout,
	}

	// Enable authentication if credentials provided.
	if len(e.User) > 0 {
		conf.Auth = smtp.AuthTypes.Plain

		clog.Debug().Msg("Using plain authentication")
	}

	url := conf.GetURL().String()

	clog.Debug().
		Bool("tls_skip", e.tlsSkipVerify).
		Bool("auth_enabled", len(e.User) > 0).
		Msg("Generated SMTP URL")

	if clog.GetLevel() <= zerolog.TraceLevel {
		clog.Trace().
			Str("url", redact.ServiceURL(url)).
			Msg("Generated SMTP URL")
	}

	return url, nil
}
