package logcapture

import (
	"bytes"
	"sync"
	"time"

	"github.com/rs/zerolog"

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
