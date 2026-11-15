package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingWriter is a destination whose every write fails part way.
type failingWriter struct {
	// written is how many bytes each write reports before it fails.
	written int
}

// errWriteFailed is the error every write to a failingWriter returns.
var errWriteFailed = errors.New("write failed")

// Write fails with errWriteFailed after the bytes it is set to report.
//
// Returns:
//   - int: The bytes set in the writer, or the length of p if shorter.
//   - error: Always errWriteFailed.
func (w failingWriter) Write(p []byte) (int, error) {
	return min(w.written, len(p)), errWriteFailed
}

// TestConfigureWriter_JSON verifies that the json format writes events to
// stderr through the escaping JSON writer.
func TestConfigureWriter_JSON(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("json", false)
	require.NoError(t, err)
	assert.Equal(t, jsonWriter{out: os.Stderr}, w)
}

// TestJSONWriter verifies that characters beyond ASCII that change how text
// shows are written as escapes and everything else passes through unchanged.
func TestJSONWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event string
		want  string
	}{
		{name: "ASCII unchanged", event: `{"message":"done"}` + "\n", want: `{"message":"done"}` + "\n"},
		{name: "printable text unchanged", event: `{"message":"héllo wörld"}`, want: `{"message":"héllo wörld"}`},
		{name: "8-bit control sequence", event: `{"error":"a` + "\u009b" + `31m"}`, want: `{"error":"a\u009b31m"}`},
		{name: "next line", event: `{"error":"a` + "\u0085" + `b"}`, want: `{"error":"a\u0085b"}`},
		{name: "bidirectional override", event: `{"error":"abc` + "\u202e" + `def"}`, want: `{"error":"abc\u202edef"}`},
		{name: "zero-width space", event: `{"error":"pass` + "\u200b" + `word"}`, want: `{"error":"pass\u200bword"}`},
		{name: "line separator", event: `{"error":"a` + "\u2028" + `b"}`, want: `{"error":"a\u2028b"}`},
		{name: "tag character as a surrogate pair", event: `{"error":"ok` + "\U000e0041" + `"}`, want: `{"error":"ok\udb40\udc41"}`},
		{name: "in a key", event: `{"` + "\u202e" + `":"v"}`, want: `{"\u202e":"v"}`},
		{name: "invalid UTF-8 kept", event: "{\"error\":\"a\xff\u0085\"}", want: "{\"error\":\"a\xff\\u0085\"}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			n, err := JSONWriter(&out).Write([]byte(tt.event))
			require.NoError(t, err)
			assert.Equal(t, len(tt.event), n)
			assert.Equal(t, tt.want, out.String())
		})
	}
}

// TestJSONWriter_destinationError verifies that a failed write returns the
// destination's error, with the destination's count for an event written as it
// is and 0 for an escaped one, whose count has no position in the event.
func TestJSONWriter_destinationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event string
		want  int
	}{
		{name: "unchanged event", event: `{"message":"done"}`, want: 3},
		{name: "escaped event", event: `{"error":"a` + "\u202e" + `"}`, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			n, err := JSONWriter(failingWriter{written: 3}).Write([]byte(tt.event))
			require.ErrorIs(t, err, errWriteFailed)
			assert.Equal(t, tt.want, n)
		})
	}
}

// TestConfigureWriter_Pretty verifies that the pretty format writes colored
// console output to stderr unless color is turned off.
func TestConfigureWriter_Pretty(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("pretty", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.False(t, cw.NoColor)
	assert.Equal(t, os.Stderr, cw.Out)

	wNoColor, err := ConfigureWriter("pretty", true)
	require.NoError(t, err)

	cwNoColor, ok := wNoColor.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.True(t, cwNoColor.NoColor)
}

// Test_newPrettyWriter_messages verifies that the pretty format quotes a
// message that holds a character that changes how text shows, with or without
// color, and writes any other message as it is.
func Test_newPrettyWriter_messages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		noColor bool
		want    string
	}{
		{name: "plain text unchanged", message: "Found new image", noColor: true, want: "WRN Found new image"},
		{name: "multi-byte text unchanged", message: "héllo wörld", noColor: true, want: "WRN héllo wörld"},
		{name: "terminal escape sequence", message: "a\x1b]0;title\ab", noColor: true, want: `WRN "a\x1b]0;title\ab"`},
		{name: "8-bit control sequence", message: "a\u009b31mb", noColor: true, want: `WRN "a\u009b31mb"`},
		{name: "bidirectional override", message: "abc\u202edef", noColor: true, want: `WRN "abc\u202edef"`},
		{name: "newline", message: "line one\nline two", noColor: true, want: `WRN "line one\nline two"`},
		{name: "bidirectional override in color", message: "abc\u202edef", noColor: false, want: `"abc\u202edef"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			writer := newPrettyWriter(tt.noColor)
			writer.Out = &out

			log := zerolog.New(writer)
			log.Warn().Msg(tt.message)

			assert.Contains(t, out.String(), tt.want)
		})
	}
}

// TestConfigureWriter_Logfmt verifies that the logfmt format writes uncolored
// key=value output to stderr, with the time, level, and message first.
func TestConfigureWriter_Logfmt(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("logfmt", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.True(t, cw.NoColor)
	assert.Equal(t, os.Stderr, cw.Out)
	assert.NotNil(t, cw.FormatTimestamp)
	assert.NotNil(t, cw.FormatLevel)
	assert.NotNil(t, cw.FormatMessage)
	assert.NotNil(t, cw.FormatFieldName)
	assert.Equal(t, []string{
		zerolog.TimestampFieldName,
		zerolog.LevelFieldName,
		zerolog.MessageFieldName,
	}, cw.PartsOrder)

	// Custom formatters emit key=value style fragments.
	assert.Contains(t, cw.FormatLevel("info"), "level=")
	assert.Contains(t, cw.FormatMessage("hello world"), "msg=")
	assert.Contains(t, cw.FormatFieldName("container"), "container=")
}

// TestConfigureWriter_LogfmtWriteThrough writes a real event through the logfmt
// ConsoleWriter and asserts single-layer quoting for spaced messages and fields.
func TestConfigureWriter_LogfmtWriteThrough(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("logfmt", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)

	buf := &bytes.Buffer{}
	cw.Out = buf

	log := zerolog.New(cw).Level(zerolog.InfoLevel).With().Timestamp().Logger()
	log.Info().
		Str("container", "nginx with spaces").
		Msg("hello world")

	line := strings.TrimSpace(buf.String())
	t.Logf("logfmt line: %s", line)

	assert.Contains(t, line, "level=info")
	// Message with spaces: single pair of quotes via FormatMessage.
	assert.Contains(t, line, `msg="hello world"`)
	// Field with spaces: ConsoleWriter pre-quotes once. FormatFieldValue must not re-quote.
	assert.Contains(t, line, `container="nginx with spaces"`)
	assert.NotContains(t, line, `\"nginx with spaces\"`)
	assert.NotContains(t, line, `container="\"nginx`)
}

// TestLogfmtWriter_messages verifies that the logfmt format quotes a message
// that holds a character that changes how text shows, even without white
// space, and writes a plain one-word message as it is.
func TestLogfmtWriter_messages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    string
	}{
		{name: "plain word unchanged", message: "ready", want: "msg=ready"},
		{name: "bidirectional override", message: "abc\u202edef", want: `msg="abc\u202edef"`},
		{name: "zero-width space", message: "ab\u200bc", want: `msg="ab\u200bc"`},
		{name: "tag character", message: "ok\U000e0041", want: `msg="ok\U000e0041"`},
		{name: "8-bit control sequence", message: "a\u009b31mb", want: `msg="a\u009b31mb"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			log := zerolog.New(LogfmtWriter(&out))
			log.Warn().Msg(tt.message)

			assert.Contains(t, out.String(), tt.want)
		})
	}
}

// Test_escapeFields verifies that the logfmt and pretty formats escape every
// character that changes how text shows in a field name or a JSON-encoded
// value, and leave strings and numbers for the console writer to quote.
func Test_escapeFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event map[string]any
		want  map[string]any
	}{
		{name: "string unchanged", event: map[string]any{"a": "x\u202ey"}, want: map[string]any{"a": "x\u202ey"}},
		{name: "number unchanged", event: map[string]any{"a": json.Number("42")}, want: map[string]any{"a": json.Number("42")}},
		{name: "bool as JSON", event: map[string]any{"a": true}, want: map[string]any{"a": json.RawMessage("true")}},
		{name: "list unchanged", event: map[string]any{"a": []any{"héllo"}}, want: map[string]any{"a": json.RawMessage(`["héllo"]`)}},
		{name: "bidirectional override in a list", event: map[string]any{"error": []any{"abc\u202edef"}}, want: map[string]any{"error": json.RawMessage(`["abc\u202edef"]`)}},
		{name: "next line in an object", event: map[string]any{"a": map[string]any{"b": "x\u0085y"}}, want: map[string]any{"a": json.RawMessage(`{"b":"x\u0085y"}`)}},
		{name: "delete in a list", event: map[string]any{"a": []any{"a\x7fb"}}, want: map[string]any{"a": json.RawMessage(`["a\u007fb"]`)}},
		{name: "terminal escape sequence in a name", event: map[string]any{"a\x1b[31m": "v"}, want: map[string]any{`"a\x1b[31m"`: "v"}},
		{
			name:  "name equal to another's quoted form",
			event: map[string]any{"a\u202eb": "x", `"a\u202eb"`: "y"},
			want:  map[string]any{`"a\u202eb"`: "x", `"\"a\\u202eb\""`: "y"},
		},
		{name: "bidirectional override in the time", event: map[string]any{"time": "1\u202e2"}, want: map[string]any{"time": `"1\u202e2"`}},
		{name: "numeric time unchanged", event: map[string]any{"time": json.Number("1700000000")}, want: map[string]any{"time": json.Number("1700000000")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, escapeFields(tt.event))
			assert.Equal(t, tt.want, tt.event)
		})
	}
}

// Test_escapeFields_stringTime verifies that the logfmt and pretty formats
// write a string time that holds a character that changes how text shows
// escaped, since the console writer writes the time as it is.
func Test_escapeFields_stringTime(t *testing.T) {
	t.Parallel()

	for name, writer := range map[string]zerolog.ConsoleWriter{
		"logfmt": LogfmtWriter(nil),
		"pretty": newPrettyWriter(true),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			writer.Out = &out

			log := zerolog.New(writer)
			log.Warn().Str(zerolog.TimestampFieldName, "1\u202e2").Msg("done")

			assert.Contains(t, out.String(), `"1\u202e2"`)
			assert.NotContains(t, out.String(), "\u202e")
		})
	}
}

// TestConfigureWriter_LogfmtTypedFields verifies that bools and slices render as
// readable text instead of Go's default decimal byte-slice form.
func TestConfigureWriter_LogfmtTypedFields(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("logfmt", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)

	buf := &bytes.Buffer{}
	cw.Out = buf

	log := zerolog.New(cw).Level(zerolog.DebugLevel).With().Timestamp().Logger()
	log.Debug().
		Bool("legacy_template", true).
		Bool("enable_label", false).
		Strs("names", []string{"watchtower-test"}).
		Strs("empty_names", nil).
		Int("entries_count", 3).
		Msg("typed fields")

	line := strings.TrimSpace(buf.String())
	t.Logf("logfmt line: %s", line)

	assert.Contains(t, line, "legacy_template=true")
	assert.Contains(t, line, "enable_label=false")
	assert.Contains(t, line, `names=["watchtower-test"]`)
	assert.Contains(t, line, "entries_count=3")
	// fmt.Sprint on JSON []byte used to emit decimal slices like [116 114 117 101].
	assert.NotContains(t, line, "[116 114 117 101]")
	assert.NotContains(t, line, "[102 97 108 115 101]")
	assert.NotContains(t, line, "[91 34")
}

// TestConfigureWriter_Auto_NoColorEnv verifies that the auto format falls back
// to logfmt when NO_COLOR is set.
//
// The test sets an environment variable, so it does not run in parallel.
func TestConfigureWriter_Auto_NoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")

	w, err := ConfigureWriter("auto", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	// auto falls back to logfmt when NO_COLOR is set.
	assert.True(t, cw.NoColor)
	assert.NotNil(t, cw.FormatTimestamp)
}

// TestConfigureWriter_Auto_EmptyNoColorEnv verifies that the auto format falls
// back to logfmt when NO_COLOR is present, even when it is empty.
//
// The test sets an environment variable, so it does not run in parallel.
func TestConfigureWriter_Auto_EmptyNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	w, err := ConfigureWriter("auto", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.True(t, cw.NoColor)
	assert.NotNil(t, cw.FormatTimestamp)
}

// TestConfigureWriter_Auto_NoColorFlag verifies that the auto format writes
// uncolored output when color is turned off.
func TestConfigureWriter_Auto_NoColorFlag(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("auto", true)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.True(t, cw.NoColor)
}

// TestConfigureWriter_CaseInsensitive verifies that format names are matched
// case-insensitively.
func TestConfigureWriter_CaseInsensitive(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("JSON", false)
	require.NoError(t, err)
	assert.Equal(t, jsonWriter{out: os.Stderr}, w)
}

// TestConfigureWriter_Invalid verifies that an unknown format is an invalid
// log format error.
func TestConfigureWriter_Invalid(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("cowsay", false)
	require.ErrorIs(t, err, errInvalidLogFormat)
	assert.Nil(t, w)
	assert.Contains(t, err.Error(), `"cowsay"`)
}

// TestConfigureWriter_Empty verifies that an empty format is an invalid log
// format error.
func TestConfigureWriter_Empty(t *testing.T) {
	t.Parallel()

	w, err := ConfigureWriter("", false)
	require.ErrorIs(t, err, errInvalidLogFormat)
	assert.Nil(t, w)
	assert.Contains(t, err.Error(), `""`)
}
