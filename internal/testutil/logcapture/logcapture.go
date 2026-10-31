package logcapture

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
)

// Buffer is a buffer that is safe for concurrent writes, for logs that
// goroutines started by a test write while the test reads them.
type Buffer struct {
	// mu guards buf.
	mu sync.Mutex
	// buf holds the written bytes.
	buf bytes.Buffer
}

// Entry is one JSON log entry decoded into its fields, keyed by field name.
type Entry = map[string]any

// New returns a logfmt logger at level that writes to a new Buffer, in the
// format production logs use.
//
// Parameters:
//   - level: Lowest level the logger writes.
//
// Returns:
//   - *zerolog.Logger: Logger writing to the buffer.
//   - *Buffer: Buffer holding the logged lines.
func New(level zerolog.Level) (*zerolog.Logger, *Buffer) {
	buf := &Buffer{}
	w := logging.LogfmtWriter(buf)
	w.TimeFormat = time.RFC3339

	return new(zerolog.New(w).Level(level).With().Timestamp().Logger()), buf
}

// NewJSON returns a JSON logger at level that writes to a new Buffer, for
// tests that read the logged fields with [Entries], [Levels] or [Messages].
//
// Parameters:
//   - level: Lowest level the logger writes.
//
// Returns:
//   - *zerolog.Logger: Logger writing to the buffer.
//   - *Buffer: Buffer holding the logged lines.
func NewJSON(level zerolog.Level) (*zerolog.Logger, *Buffer) {
	buf := &Buffer{}

	return new(zerolog.New(buf).Level(level).With().Timestamp().Logger()), buf
}

// Entries decodes each line of JSON log output into a T, such as an [Entry]
// or a struct with the fields a test reads. A line that does not decode fails
// the test.
//
// Parameters:
//   - tb: Test to fail on a line that does not decode.
//   - logs: JSON log output, one entry per line.
//
// Returns:
//   - []T: The decoded entries, in the order they were logged.
func Entries[T any](tb testing.TB, logs string) []T {
	tb.Helper()

	var entries []T

	for line := range strings.Lines(logs) {
		var entry T

		require.NoError(tb, json.Unmarshal([]byte(line), &entry), "log line: %s", line)

		entries = append(entries, entry)
	}

	return entries
}

// Levels returns the level of each JSON log entry with the given message, in
// the order they were logged.
//
// Parameters:
//   - tb: Test to fail on a line that does not decode.
//   - logs: JSON log output, one entry per line.
//   - message: Message of the entries to report.
//
// Returns:
//   - []string: Levels such as "warn" or "debug".
func Levels(tb testing.TB, logs, message string) []string {
	tb.Helper()

	var levels []string

	for _, entry := range Entries[Entry](tb, logs) {
		if entry["message"] == message {
			level, ok := entry["level"].(string)
			require.True(tb, ok, "entry has no level: %v", entry)

			levels = append(levels, level)
		}
	}

	return levels
}

// Messages returns the message of each JSON log entry at the given level, in
// the order they were logged.
//
// Parameters:
//   - tb: Test to fail on a line that does not decode.
//   - logs: JSON log output, one entry per line.
//   - level: Level of the entries to report, such as "warn".
//
// Returns:
//   - []string: The entries' messages.
func Messages(tb testing.TB, logs, level string) []string {
	tb.Helper()

	var messages []string

	for _, entry := range Entries[Entry](tb, logs) {
		if entry["level"] == level {
			message, ok := entry["message"].(string)
			require.True(tb, ok, "entry has no message: %v", entry)

			messages = append(messages, message)
		}
	}

	return messages
}

// Write appends p to the buffer.
//
// Parameters:
//   - p: Bytes to append.
//
// Returns:
//   - int: Number of bytes written.
//   - error: Always nil.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// bytes.Buffer.Write always returns a nil error.
	n, _ := b.buf.Write(p)

	return n, nil
}

// String returns everything written so far.
//
// Returns:
//   - string: The buffer's contents.
func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}
