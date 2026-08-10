// Package logging provides zerolog construction and configuration helpers for Watchtower.
//
// This package holds no logger state. It does not expose a global logger, Global
// accessor, InitLogger, or init-based setup. Callers construct a *zerolog.Logger
// via New and related helpers and pass it explicitly to subsystems.
package logging

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/rs/zerolog"
)

// ErrUnknownLogLevel is returned by ParseLevel for unrecognized level strings.
var ErrUnknownLogLevel = errors.New("unknown log level")

// Level maps to zerolog levels.
type Level uint8

// Logging levels ordered from most to least verbose.
const (
	TraceLevel Level = iota
	DebugLevel
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
	PanicLevel
)

// toZerologLevel converts a logging.Level to zerolog.Level.
//
// Parameters:
//   - level: Application logging level constant.
//
// Returns:
//   - zerolog.Level: Corresponding zerolog level, or InfoLevel for unknown values.
func toZerologLevel(level Level) zerolog.Level {
	switch level {
	case TraceLevel:
		return zerolog.TraceLevel
	case DebugLevel:
		return zerolog.DebugLevel
	case InfoLevel:
		return zerolog.InfoLevel
	case WarnLevel:
		return zerolog.WarnLevel
	case ErrorLevel:
		return zerolog.ErrorLevel
	case FatalLevel:
		return zerolog.FatalLevel
	case PanicLevel:
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// New creates a *zerolog.Logger configured with the given writer and level.
//
// The logger always includes timestamps on events.
//
// Parameters:
//   - w: Destination for log output.
//   - level: Minimum level of events to emit.
//
// Returns:
//   - *zerolog.Logger: Configured logger instance.
func New(w io.Writer, level Level) *zerolog.Logger {
	l := zerolog.New(w).Level(toZerologLevel(level)).With().Timestamp().Logger()

	return &l
}

// With returns a child logger with a single field added.
//
// The returned logger is a new instance (immutable style). Prefer native zerolog
// event chaining at call sites (log.Debug().Str(...).Msg(...)) when attaching
// fields for a single log line. Use With or WithFields when building a scoped
// child logger that is reused across multiple statements.
//
// Parameters:
//   - log: Parent logger.
//   - key: Field name.
//   - val: Field value.
//
// Returns:
//   - *zerolog.Logger: Child logger with the field applied.
func With(log *zerolog.Logger, key string, val any) *zerolog.Logger {
	l := log.With().Interface(key, val).Logger()

	return &l
}

// WithFields returns a child logger with all fields added.
//
// The returned logger is a new instance (immutable style). Prefer
// log.With().Fields(fields).Logger() or per-event Fields when that reads more
// clearly. WithFields remains available for shared scoped loggers.
//
// Parameters:
//   - log: Parent logger.
//   - fields: Map of field names to values.
//
// Returns:
//   - *zerolog.Logger: Child logger with all fields applied.
func WithFields(log *zerolog.Logger, fields map[string]any) *zerolog.Logger {
	ctx := log.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}

	l := ctx.Logger()

	return &l
}

// WithError returns a child logger with the error field added when err is non-nil.
//
// When err is nil, the original logger is returned unchanged. Prefer
// log.Debug().Err(err).Msg(...) (or the appropriate level) for a single log line.
//
// Parameters:
//   - log: Parent logger.
//   - err: Error to attach, or nil.
//
// Returns:
//   - *zerolog.Logger: Child logger with error field, or log unchanged when err is nil.
func WithError(log *zerolog.Logger, err error) *zerolog.Logger {
	if err == nil {
		return log
	}

	l := log.With().Err(err).Logger()

	return &l
}

// ParseLevel maps a CLI level string to zerolog.Level.
//
// Accepted values (case-insensitive) are panic, fatal, error, warn, warning,
// info, debug, and trace.
//
// Parameters:
//   - level: Level name string (may include surrounding whitespace).
//
// Returns:
//   - zerolog.Level: Parsed level, or NoLevel when invalid.
//   - error: Non-nil when the level string is not recognized.
func ParseLevel(level string) (zerolog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "panic":
		return zerolog.PanicLevel, nil
	case "fatal":
		return zerolog.FatalLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	case "warn", "warning":
		return zerolog.WarnLevel, nil
	case "info":
		return zerolog.InfoLevel, nil
	case "debug":
		return zerolog.DebugLevel, nil
	case "trace":
		return zerolog.TraceLevel, nil
	default:
		return zerolog.NoLevel, fmt.Errorf("%w: %q", ErrUnknownLogLevel, level)
	}
}

// isTruthy reports whether value is a truthy CLI or env-style flag string.
//
// Empty, "0", "f", "false", "n", "no", and "off" (case-insensitive) are falsey.
//
// Parameters:
//   - value: Flag or environment value to interpret.
//
// Returns:
//   - bool: True when the value should be treated as enabled.
func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "f", "false", "n", "no", "off":
		return false
	default:
		return true
	}
}

// ConfigureLevel applies CLI level aliases (--debug, --trace) to a zerolog logger.
//
// Priority is traceFlag, then debugFlag, then rawLevel. When rawLevel is empty and
// neither alias is set, the logger is returned unchanged. Invalid rawLevel leaves
// the logger unchanged without returning an error.
//
// Callers that must fail fast on a bad --log-level (for example flags.SetupLogging
// or composition-root wiring) must validate with ParseLevel first and surface that
// error. ConfigureLevel alone will not reject invalid values.
//
// Parameters:
//   - log: Logger to reconfigure.
//   - rawLevel: Explicit level string from --log-level (may be empty).
//   - debugFlag: Truthy when --debug is set.
//   - traceFlag: Truthy when --trace is set.
//
// Returns:
//   - *zerolog.Logger: Logger with the resolved level applied.
func ConfigureLevel(log *zerolog.Logger, rawLevel, debugFlag, traceFlag string) *zerolog.Logger {
	if isTruthy(traceFlag) {
		l := log.Level(zerolog.TraceLevel)

		return &l
	}

	if isTruthy(debugFlag) {
		l := log.Level(zerolog.DebugLevel)

		return &l
	}

	if strings.TrimSpace(rawLevel) == "" {
		return log
	}

	level, err := ParseLevel(rawLevel)
	if err != nil {
		return log
	}

	l := log.Level(level)

	return &l
}
