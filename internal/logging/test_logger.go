package logging

import (
	"bytes"
	"io"
	"time"

	"github.com/rs/zerolog"
)

// LogfmtWriter returns a ConsoleWriter that emits logfmt-style lines to out.
//
// Intended for tests and any caller that wants logfmt on a custom sink.
//
// Parameters:
//   - out: Destination for formatted log lines.
//
// Returns:
//   - zerolog.ConsoleWriter: Logfmt-style console writer writing to out.
func LogfmtWriter(out io.Writer) zerolog.ConsoleWriter {
	w := newLogfmtWriter()
	w.Out = out
	w.TimeFormat = time.RFC3339

	return w
}

// NewTestLogger constructs a *zerolog.Logger writing logfmt lines to a buffer.
//
// Level maps via the same Level constants as New.
//
// Parameters:
//   - level: Minimum level of events to capture.
//
// Returns:
//   - *zerolog.Logger: Logger writing to the returned buffer.
//   - *bytes.Buffer: Buffer containing captured log output.
func NewTestLogger(level Level) (*zerolog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := zerolog.New(LogfmtWriter(buf)).Level(toZerologLevel(level)).With().Timestamp().Logger()

	return &l, buf
}

// NopLogger returns a discarded *zerolog.Logger for tests that do not assert on logs.
//
// Returns:
//   - *zerolog.Logger: Logger that discards all events.
func NopLogger() *zerolog.Logger {
	n := zerolog.Nop()

	return &n
}
