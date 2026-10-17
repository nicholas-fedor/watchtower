// Package logtest provides loggers for tests.
//
// Loggers write logfmt lines with the formatting Watchtower uses in production,
// so tests can assert on log output as users see it.
package logtest

import (
	"bytes"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/logging"
)

// New returns a logger that writes logfmt lines to the returned buffer.
//
// Parameters:
//   - level: Minimum level of events to capture.
//
// Returns:
//   - *zerolog.Logger: Logger writing to the returned buffer.
//   - *bytes.Buffer: Buffer containing the captured log output.
func New(level logging.Level) (*zerolog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}

	return logging.New(logging.LogfmtWriter(buf), level), buf
}

// Nop returns a logger that discards every event, for tests that do not
// assert on logs.
//
// Returns:
//   - *zerolog.Logger: Logger that discards all events.
func Nop() *zerolog.Logger {
	log := zerolog.Nop()

	return &log
}
