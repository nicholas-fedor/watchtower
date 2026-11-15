package logging

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
)

// jsonWriter writes zerolog's JSON events to out with every character that
// changes how text shows escaped.
type jsonWriter struct {
	// out receives the events.
	out io.Writer
}

// errInvalidLogFormat is returned by ConfigureWriter for unrecognized format names.
var errInvalidLogFormat = errors.New("invalid log format")

// ConfigureWriter returns an io.Writer for the requested log format.
//
// Supported formats (case-insensitive):
//   - json: zerolog's JSON encoding through [JSONWriter] to os.Stderr
//   - pretty: zerolog.ConsoleWriter with colors unless noColor is true
//   - logfmt: ConsoleWriter with NoColor and key=value-style formatting
//   - auto: pretty when stderr is a TTY, NO_COLOR is not present in the
//     environment (presence includes empty value), and noColor is false.
//     Otherwise logfmt is used.
//
// Parameters:
//   - format: Requested format name (json, pretty, logfmt, or auto).
//   - noColor: When true, disables colorized pretty output.
//
// Returns:
//   - io.Writer: Writer suitable for zerolog.New.
//   - error: Non-nil when format is not recognized.
func ConfigureWriter(format string, noColor bool) (io.Writer, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return JSONWriter(os.Stderr), nil
	case "pretty":
		return newPrettyWriter(noColor), nil
	case "logfmt":
		return LogfmtWriter(os.Stderr), nil
	case "auto":
		if usePrettyAuto(noColor) {
			return newPrettyWriter(false), nil
		}

		return LogfmtWriter(os.Stderr), nil
	default:
		return nil, fmt.Errorf("%w: %q", errInvalidLogFormat, format)
	}
}

// JSONWriter returns a writer that passes zerolog's JSON events to out with C1
// control characters, Unicode format characters, and line and paragraph
// separators written as \u escapes.
//
// zerolog escapes only ASCII control characters. Without these escapes, text
// from outside Watchtower such as a registry error could carry an 8-bit
// terminal control or a bidirectional override into a terminal or log viewer.
// Each event stays valid JSON with the same values.
//
// Parameters:
//   - out: Destination for the events.
//
// Returns:
//   - io.Writer: Writer that expects one encoded event per Write, as zerolog
//     writes them.
func JSONWriter(out io.Writer) io.Writer {
	return jsonWriter{out: out}
}

// Write writes event to the destination with every character that changes
// how text shows escaped.
//
// Parameters:
//   - event: One JSON event encoded by zerolog.
//
// Returns:
//   - int: Length of event when it is written in full. On an error, at most
//     the bytes of event that reached the destination.
//   - error: Error from the destination.
func (w jsonWriter) Write(event []byte) (int, error) {
	// zerolog has already escaped the ASCII characters that need it. Most events
	// are ASCII throughout, so the search starts at the first byte beyond ASCII.
	rest := event
	for len(rest) > 0 && rest[0] < utf8.RuneSelf {
		rest = rest[1:]
	}

	if !bytes.ContainsFunc(rest, needsJSONEscape) {
		return w.out.Write(event) //nolint:wrapcheck // The destination's error needs no context.
	}

	// Outside its strings, JSON holds only ASCII. Every character found here is
	// inside a string, where an escape keeps its value.
	escaped := append(make([]byte, 0, len(event)), event[:len(event)-len(rest)]...)

	var units [2]uint16

	for len(rest) > 0 {
		char, size := utf8.DecodeRune(rest)

		if needsJSONEscape(char) {
			for _, unit := range utf16.AppendRune(units[:0], char) {
				escaped = fmt.Appendf(escaped, `\u%04x`, unit)
			}
		} else {
			escaped = append(escaped, rest[:size]...)
		}

		rest = rest[size:]
	}

	_, err := w.out.Write(escaped)
	if err != nil {
		return 0, err //nolint:wrapcheck // The destination's error needs no context.
	}

	return len(event), nil
}

// usePrettyAuto reports whether auto format should use colorized pretty output.
//
// NO_COLOR follows this repo's presence semantics (any set value, including empty
// string), matching internal/settings ApplyEnvToFlags rather than only non-empty values.
//
// Parameters:
//   - noColor: Explicit --no-color flag value.
//
// Returns:
//   - bool: True when pretty colored output should be used for auto format.
func usePrettyAuto(noColor bool) bool {
	if noColor {
		return false
	}

	if _, set := os.LookupEnv("NO_COLOR"); set {
		return false
	}

	fd := os.Stderr.Fd()

	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// newPrettyWriter builds a human-friendly ConsoleWriter.
//
// Parameters:
//   - noColor: When true, disables ANSI colors.
//
// Returns:
//   - zerolog.ConsoleWriter: Pretty console writer targeting stderr.
func newPrettyWriter(noColor bool) zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{
		Out:        os.Stderr,
		NoColor:    noColor,
		TimeFormat: time.Kitchen,
	}
}

// LogfmtWriter builds a ConsoleWriter that emits key=value logfmt-style lines to out.
//
// Parameters:
//   - out: Destination for formatted log lines.
//
// Returns:
//   - zerolog.ConsoleWriter: Logfmt-style console writer writing to out.
func LogfmtWriter(out io.Writer) zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{
		Out:        out,
		NoColor:    true,
		TimeFormat: time.RFC3339,
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.LevelFieldName,
			zerolog.MessageFieldName,
		},
		FormatTimestamp: func(i any) string {
			if i == nil {
				return "time="
			}

			return "time=" + fmt.Sprint(i)
		},
		FormatLevel: func(i any) string {
			if i == nil {
				return "level="
			}

			return "level=" + strings.ToLower(fmt.Sprint(i))
		},
		FormatMessage: func(i any) string {
			if i == nil {
				return "msg="
			}

			s := fmt.Sprint(i)
			if needsQuote(s) {
				return "msg=" + fmt.Sprintf("%q", s)
			}

			return "msg=" + s
		},
		FormatFieldName: func(i any) string {
			return fmt.Sprint(i) + "="
		},
		// FormatFieldValue must preserve ConsoleWriter pre-formatting. Strings may
		// already be strconv.Quoted when they need quotes. Non-string values (bools,
		// slices, maps) are JSON-marshaled to []byte before this runs. See zerolog
		// ConsoleWriter.writeFields. Render []byte as text so true prints as true
		// rather than as a decimal byte slice.
		FormatFieldValue: formatLogfmtFieldValue,
	}
}

// formatLogfmtFieldValue renders a ConsoleWriter field value for logfmt output.
//
// Parameters:
//   - i: Field value from ConsoleWriter (string, json.Number, or JSON []byte).
//
// Returns:
//   - string: Printable field value without additional quoting.
func formatLogfmtFieldValue(i any) string {
	switch v := i.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	default:
		// Match zerolog consoleDefaultFormatFieldValue for remaining types.
		return fmt.Sprintf("%s", i)
	}
}

// needsJSONEscape reports whether [JSONWriter] escapes char.
//
// Parameters:
//   - char: Character to check.
//
// Returns:
//   - bool: True for a character beyond ASCII that changes how text shows.
func needsJSONEscape(char rune) bool {
	// zerolog escapes the ASCII ones itself.
	return char > unicode.MaxASCII && changesDisplay(char)
}

// needsQuote reports whether a logfmt value should be quoted.
//
// Parameters:
//   - s: Field or message value.
//
// Returns:
//   - bool: True when the value contains spaces, equals signs, quotes, is empty, or contains Unicode whitespace or control characters.
func needsQuote(s string) bool {
	if s == "" {
		return true
	}

	for _, r := range s {
		if r == ' ' || r == '=' || r == '"' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}

	return false
}
