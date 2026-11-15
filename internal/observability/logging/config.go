package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"strconv"
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
//   - int: Bytes of event written. For an event that needed escapes, its
//     length, or 0 on an error.
//   - error: Error from the destination.
func (w jsonWriter) Write(event []byte) (int, error) {
	escaped := escapeJSON(event)

	written, err := w.out.Write(escaped)
	if len(escaped) == len(event) {
		// An escape is always longer than the character it replaces. Equal
		// lengths mean nothing was escaped and the destination counted bytes of
		// event.
		return written, err //nolint:wrapcheck // The destination's error needs no context.
	}

	if err != nil {
		return 0, err //nolint:wrapcheck // The destination's error needs no context.
	}

	return len(event), nil
}

// usePrettyAuto reports whether auto format should use colorized pretty output.
//
// NO_COLOR follows this repo's presence semantics (any set value, including empty
// string), as the settings package applies it to the no-color flag, rather than only non-empty values.
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
// A message, field name or field value that holds a character that changes
// how text shows is quoted or escaped.
//
// Parameters:
//   - noColor: When true, disables ANSI colors.
//
// Returns:
//   - zerolog.ConsoleWriter: Pretty console writer targeting stderr.
func newPrettyWriter(noColor bool) zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{
		Out:           os.Stderr,
		NoColor:       noColor,
		TimeFormat:    time.Kitchen,
		FormatPrepare: escapePrettyEvent,
	}
}

// escapePrettyEvent prepares a decoded event for the pretty format, with its
// fields escaped and its message quoted when it holds a character that changes
// how text shows.
//
// Parameters:
//   - evt: Decoded event, which the console writer formats after this returns.
//
// Returns:
//   - error: Always nil.
func escapePrettyEvent(evt map[string]any) error {
	// The console writer writes the message as it is. Quoting the value here
	// keeps the writer's own message formatting, which makes messages at info
	// and above bold.
	message, ok := evt[zerolog.MessageFieldName].(string)
	if ok && strings.ContainsFunc(message, changesDisplay) {
		evt[zerolog.MessageFieldName] = strconv.Quote(message)
	}

	return escapeFields(evt)
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
				return "msg=" + strconv.Quote(s)
			}

			return "msg=" + s
		},
		FormatFieldName: func(i any) string {
			return fmt.Sprint(i) + "="
		},
		FormatPrepare: escapeFields,
	}
}

// escapeFields prepares a decoded event for the logfmt and pretty formats by
// escaping every character that changes how text shows in its field names, its
// time, level and caller, and its values other than strings and numbers.
//
// Parameters:
//   - evt: Decoded event, which the console writer formats after this returns.
//
// Returns:
//   - error: Always nil.
func escapeFields(evt map[string]any) error {
	// The console writer quotes a field's string value that needs it and writes
	// a number as it is. It writes field names and the time, level and caller
	// as they are. It re-encodes any other value as JSON, which leaves DEL and
	// characters beyond ASCII raw. A quoted name always starts with a quote, so
	// quoting every name that starts with one too keeps names distinct. Renamed
	// fields join the event once every original name is gone.
	var renamed map[string]any

	for name, value := range evt {
		switch typed := value.(type) {
		case json.Number:
		case string:
			part := name == zerolog.TimestampFieldName || name == zerolog.LevelFieldName ||
				name == zerolog.CallerFieldName
			if part && strings.ContainsFunc(typed, changesDisplay) {
				value = strconv.Quote(typed)
			}
		default:
			encoded, err := zerolog.InterfaceMarshalFunc(value)
			if err == nil {
				value = json.RawMessage(escapeJSON(encoded))
			}
		}

		if strings.HasPrefix(name, `"`) || strings.ContainsFunc(name, changesDisplay) {
			if renamed == nil {
				renamed = make(map[string]any)
			}

			delete(evt, name)
			renamed[strconv.Quote(name)] = value

			continue
		}

		evt[name] = value
	}

	maps.Copy(evt, renamed)

	return nil
}

// escapeJSON returns encoded with every character that changes how text shows
// written as a \u escape, apart from the white space between values.
//
// Parameters:
//   - encoded: Valid JSON.
//
// Returns:
//   - []byte: encoded itself when nothing needs escaping, otherwise an escaped
//     copy that decodes to the same values.
func escapeJSON(encoded []byte) []byte {
	// Most JSON is printable ASCII throughout, which needs no escape, so the
	// search starts at the first byte from space to tilde that is not.
	rest := encoded
	for len(rest) > 0 && rest[0] >= ' ' && rest[0] <= '~' {
		rest = rest[1:]
	}

	if !bytes.ContainsFunc(rest, needsJSONEscape) {
		return encoded
	}

	// Outside its strings, valid JSON holds only printable ASCII and the white
	// space that is never escaped. Every character escaped here is inside a
	// string, where an escape keeps its value.
	escaped := append(make([]byte, 0, len(encoded)), encoded[:len(encoded)-len(rest)]...)

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

	return escaped
}

// needsJSONEscape reports whether JSON output escapes char.
//
// Parameters:
//   - char: Character to check.
//
// Returns:
//   - bool: True for a character that changes how text shows, other than tab,
//     newline and carriage return.
func needsJSONEscape(char rune) bool {
	// Valid JSON holds these three raw only as white space between values and
	// escapes them inside strings. zerolog ends each event with a newline.
	return changesDisplay(char) && char != '\t' && char != '\n' && char != '\r'
}

// needsQuote reports whether a logfmt message should be quoted.
//
// Parameters:
//   - s: Message to check.
//
// Returns:
//   - bool: True when the message is empty or holds an equals sign, a quote,
//     white space, or a character that changes how text shows.
func needsQuote(s string) bool {
	if s == "" {
		return true
	}

	for _, r := range s {
		if r == '=' || r == '"' || unicode.IsSpace(r) || changesDisplay(r) {
			return true
		}
	}

	return false
}
