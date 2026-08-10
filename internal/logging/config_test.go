package logging_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/logging"
)

func TestConfigureWriter_JSON(t *testing.T) {
	t.Parallel()

	w, err := logging.ConfigureWriter("json", false)
	require.NoError(t, err)
	assert.Equal(t, os.Stderr, w)
}

func TestConfigureWriter_Pretty(t *testing.T) {
	t.Parallel()

	w, err := logging.ConfigureWriter("pretty", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.False(t, cw.NoColor)
	assert.Equal(t, os.Stderr, cw.Out)

	wNoColor, err := logging.ConfigureWriter("pretty", true)
	require.NoError(t, err)

	cwNoColor, ok := wNoColor.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.True(t, cwNoColor.NoColor)
}

func TestConfigureWriter_Logfmt(t *testing.T) {
	t.Parallel()

	w, err := logging.ConfigureWriter("logfmt", false)
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

	w, err := logging.ConfigureWriter("logfmt", false)
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

	w, err := logging.ConfigureWriter("logfmt", false)
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

func TestConfigureWriter_Auto_NoColorEnv(t *testing.T) {
	// Not parallel: mutates NO_COLOR.
	t.Setenv("NO_COLOR", "1")

	w, err := logging.ConfigureWriter("auto", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	// auto falls back to logfmt when NO_COLOR is set.
	assert.True(t, cw.NoColor)
	assert.NotNil(t, cw.FormatTimestamp)
}

func TestConfigureWriter_Auto_EmptyNoColorEnv(t *testing.T) {
	// Presence of NO_COLOR (even empty) forces non-pretty (logfmt) for auto.
	t.Setenv("NO_COLOR", "")

	w, err := logging.ConfigureWriter("auto", false)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.True(t, cw.NoColor)
	assert.NotNil(t, cw.FormatTimestamp)
}

func TestConfigureWriter_Auto_NoColorFlag(t *testing.T) {
	t.Parallel()

	w, err := logging.ConfigureWriter("auto", true)
	require.NoError(t, err)

	cw, ok := w.(zerolog.ConsoleWriter)
	require.True(t, ok)
	assert.True(t, cw.NoColor)
}

func TestConfigureWriter_CaseInsensitive(t *testing.T) {
	t.Parallel()

	w, err := logging.ConfigureWriter("JSON", false)
	require.NoError(t, err)
	assert.Equal(t, os.Stderr, w)
}

func TestConfigureWriter_Invalid(t *testing.T) {
	t.Parallel()

	w, err := logging.ConfigureWriter("cowsay", false)
	require.Error(t, err)
	assert.Nil(t, w)
	assert.Contains(t, err.Error(), `invalid log format: "cowsay"`)
}

func TestConfigureWriter_Empty(t *testing.T) {
	t.Parallel()

	w, err := logging.ConfigureWriter("", false)
	require.Error(t, err)
	assert.Nil(t, w)
	assert.Contains(t, err.Error(), `invalid log format: ""`)
}
