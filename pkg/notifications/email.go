// Package notifications provides mechanisms for sending notifications via various services.
// This file implements email notification functionality using SMTP.
package notifications

import (
	"errors"
	"fmt"
	"time"

	"github.com/nicholas-fedor/shoutrrr/pkg/services/smtp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	emailType = "email"
)

// Errors for email notification configuration.
var (
	// errInvalidPortRange indicates that the specified SMTP port is outside the valid range (0-65535).
	errInvalidPortRange = errors.New("port out of valid range (0-65535)")
)

// emailTypeNotifier handles email notifications using SMTP configuration.
// It supports batching log entries with a configurable delay.
type emailTypeNotifier struct {
	From, To               string
	Server, User, Password string
	Port                   int
	tlsSkipVerify          bool
	entries                []*logrus.Entry
	delay                  time.Duration
}

// newEmailNotifier creates a new email notifier from command-line flags.
// It initializes SMTP settings and delay for notification batching.
func newEmailNotifier(c *cobra.Command) types.ConvertibleNotifier {
	flags := c.Flags()

	from, _ := flags.GetString("notification-email-from")
	to, _ := flags.GetString("notification-email-to") //nolint:varnamelen
	server, _ := flags.GetString("notification-email-server")
	user, _ := flags.GetString("notification-email-server-user")
	password, _ := flags.GetString("notification-email-server-password")
	port, _ := flags.GetInt("notification-email-server-port")
	tlsSkipVerify, _ := flags.GetBool("notification-email-server-tls-skip-verify")
	delay, _ := flags.GetInt("notification-email-delay")

	clog := logrus.WithFields(logrus.Fields{
		"from":          from,
		"to":            to,
		"server":        server,
		"port":          port,
		"tls_skip":      tlsSkipVerify,
		"delay_seconds": delay,
	})
	clog.Debug("Initializing email notifier from flags")

	// Only log sensitive fields (user, password) at trace level
	if logrus.IsLevelEnabled(logrus.TraceLevel) {
		clog.WithFields(logrus.Fields{
			"user":     user,
			"password": password,
		}).Trace("Email notifier credentials loaded")
	}

	return &emailTypeNotifier{
		entries:       []*logrus.Entry{},
		From:          from,
		To:            to,
		Server:        server,
		User:          user,
		Password:      password,
		Port:          port,
		tlsSkipVerify: tlsSkipVerify,
		delay:         time.Duration(delay) * time.Second,
	}
}

// GetURL generates the SMTP URL for the email notifier based on its configuration.
// It configures authentication, TLS settings, and returns the formatted URL, validating the port range.
func (e *emailTypeNotifier) GetURL(_ *cobra.Command) (string, error) {
	clog := logrus.WithFields(logrus.Fields{
		"from":   e.From,
		"to":     e.To,
		"server": e.Server,
		"port":   e.Port,
	})
	clog.Debug("Generating SMTP URL")

	// Validate port range (0-65535)
	if e.Port < 0 || e.Port > 65535 {
		clog.WithField("port", e.Port).Debug("Invalid SMTP port")

		return "", fmt.Errorf("port %d: %w", e.Port, errInvalidPortRange)
	}

	port := uint16(e.Port)

	conf := &smtp.Config{
		FromAddress: e.From,
		FromName:    "Watchtower",
		ToAddresses: []string{e.To},
		Port:        port,
		Host:        e.Server,
		Username:    e.User,
		Password:    e.Password,
		UseStartTLS: !e.tlsSkipVerify,
		UseHTML:     false,
		Encryption:  smtp.EncMethods.Auto,
		Auth:        smtp.AuthTypes.None,
		ClientHost:  "localhost",
	}

	if len(e.User) > 0 {
		conf.Auth = smtp.AuthTypes.Plain

		clog.Debug("Using plain authentication")
	}

	if e.tlsSkipVerify {
		conf.Encryption = smtp.EncMethods.None

		clog.Debug("TLS verification skipped")
	}

	url := conf.GetURL().String()
	clog.WithFields(logrus.Fields{
		"url":          url,
		"tls_skip":     e.tlsSkipVerify,
		"auth_enabled": len(e.User) > 0,
	}).Debug("Generated SMTP URL")

	return url, nil
}

// GetDelay returns the configured delay for batching email notifications.
// It provides the duration to wait before sending queued messages.
func (e *emailTypeNotifier) GetDelay() time.Duration {
	clog := logrus.WithFields(logrus.Fields{
		"from":   e.From,
		"to":     e.To,
		"server": e.Server,
		"delay":  e.delay,
	})
	clog.Debug("Retrieved email notification delay")

	return e.delay
}
