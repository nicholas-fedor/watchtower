package notify

import (
	"net/http"
	"testing"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/nicholas-fedor/watchtower/internal/testutil/httpstub"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
)

// newGenericWebhookURL returns the Shoutrrr URL of a generic webhook over HTTP
// that accepts one notification. Shoutrrr logs "got custom URL" and
// "converted custom URL" at debug for it. It logs "sending without TLS" at
// warn and "response received" at debug when sending to it.
//
// Parameters:
//   - t: Test that owns the webhook server.
//
// Returns:
//   - string: The generic+http service URL.
func newGenericWebhookURL(t *testing.T) string {
	t.Helper()

	server := httpstub.New(t)
	server.AppendHandlers(httpstub.RespondWith(http.StatusNoContent, "", nil))

	return "generic+" + server.URL()
}

// TestNewShoutrrrLogger verifies that a Shoutrrr sender with this logger
// writes only the logger service's text to the text destination, as stdout
// mode needs. Shoutrrr's own records are logged at their level. The text is
// written at every log level.
func TestNewShoutrrrLogger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		level zerolog.Level
		// debug and warn are the expected messages at those levels.
		debug []string
		warn  []string
	}{
		{
			name:  "debug level",
			level: zerolog.DebugLevel,
			debug: []string{"Shoutrrr: got custom URL", "Shoutrrr: converted custom URL", "Shoutrrr: response received"},
			warn:  []string{"Shoutrrr: sending without TLS"},
		},
		{
			name:  "info level",
			level: zerolog.InfoLevel,
			warn:  []string{"Shoutrrr: sending without TLS"},
		},
		{
			name:  "error level",
			level: zerolog.ErrorLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			records, recordsBuf := logcapture.NewJSON(tt.level)
			textBuf := &logcapture.Buffer{}

			sender, err := shoutrrr.NewSenderWithOptions(
				newShoutrrrLogger(records, textBuf, ""),
				shoutrrrTypes.SenderOptions{},
				"logger://",
				newGenericWebhookURL(t),
			)
			require.NoError(t, err)

			for _, err := range sender.Send("Found new image", &shoutrrrTypes.Params{}) {
				require.NoError(t, err)
			}

			assert.Equal(t, "Found new image\n", textBuf.String())

			logs := recordsBuf.String()
			assert.Equal(t, tt.debug, logcapture.Messages(t, logs, "debug"))
			assert.Equal(t, tt.warn, logcapture.Messages(t, logs, "warn"))
			assert.NotContains(t, logs, "Found new image")

			for _, entry := range logcapture.Entries[logcapture.Entry](t, logs) {
				assert.Equal(t, "generic", entry["service"])
			}
		})
	}
}

// TestShoutrrrLogHandler_WithGroup verifies that a group nests the attributes
// of the records the handler logs, as slog's groups do.
func TestShoutrrrLogHandler_WithGroup(t *testing.T) {
	t.Parallel()

	records, recordsBuf := logcapture.NewJSON(zerolog.InfoLevel)
	textBuf := &logcapture.Buffer{}

	logger := newShoutrrrLogger(records, textBuf, "").Slog()
	logger.WithGroup("request").Warn("sending without TLS", "status", 204)

	entries := logcapture.Entries[logcapture.Entry](t, recordsBuf.String())
	require.Len(t, entries, 1)
	assert.Equal(t, "warn", entries[0]["level"])
	assert.Equal(t, "Shoutrrr: sending without TLS", entries[0]["message"])
	assert.InDelta(t, 204, entries[0]["request.status"], 0)
	assert.Empty(t, textBuf.String())
}
