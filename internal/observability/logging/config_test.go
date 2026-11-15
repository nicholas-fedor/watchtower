package logging

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingWriter is a destination whose every write fails.
type failingWriter struct{}

// errWriteFailed is the error every write to a failingWriter returns.
var errWriteFailed = errors.New("write failed")

// Write fails with errWriteFailed.
//
// Returns:
//   - int: Always 0.
//   - error: Always errWriteFailed.
func (failingWriter) Write([]byte) (int, error) {
	return 0, errWriteFailed
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
// destination's error, whether or not the event needed escapes.
func TestJSONWriter_destinationError(t *testing.T) {
	t.Parallel()

	for _, event := range []string{`{"message":"done"}`, `{"error":"a` + "\u202e" + `"}`} {
		n, err := JSONWriter(failingWriter{}).Write([]byte(event))
		require.ErrorIs(t, err, errWriteFailed, event)
		assert.Zero(t, n, event)
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
