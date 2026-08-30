package stream

import (
	"bytes"
	"context"
	"io"
	"time"
)

const (
	// StreamStdout is Watchtower standard output.
	StreamStdout = "stdout"
	// StreamStderr is Watchtower standard error.
	StreamStderr = "stderr"
)

// Line is one log line in a case stream.
type Line struct {
	// Time is the ingest timestamp.
	Time time.Time `json:"time"`
	// Body is the line without a trailing newline.
	Body string `json:"body"`
	// Stream is stdout or stderr.
	Stream string `json:"stream"`
}

// Logs is the log-stream seam.
type Logs interface {
	// Push appends lines for one case stream.
	Push(ctx context.Context, runID, caseID, streamName string, lines []Line) error
	// Query returns lines for a case, optionally one stream.
	Query(ctx context.Context, runID, caseID, streamName string) ([]Line, error)
	// Ready reports whether the backend can accept writes.
	Ready(ctx context.Context) error
	// Close releases resources.
	Close() error
}

// Writer is an io.WriteCloser that batches lines into Logs.
type Writer struct {
	// ctx is cancellation for Push.
	ctx context.Context
	// logs is the backend.
	logs Logs
	// runID is the sitting id.
	runID string
	// caseID is the case id.
	caseID string
	// streamName is stdout or stderr.
	streamName string
	// buf holds bytes since the last newline.
	buf []byte
	// pending are complete lines not yet pushed.
	pending []Line
}

// NewWriter returns a writer that pushes complete lines to logs.
//
// Parameters:
//   - ctx: Cancellation for Push.
//   - logs: Backend.
//   - runID: Sitting id.
//   - caseID: Case id.
//   - streamName: stdout or stderr.
//
// Returns:
//   - *Writer: Line-buffered writer.
func NewWriter(ctx context.Context, logs Logs, runID, caseID, streamName string) *Writer {
	return &Writer{
		ctx:        ctx,
		logs:       logs,
		runID:      runID,
		caseID:     caseID,
		streamName: streamName,
	}
}

// Write implements io.Writer.
//
// Parameters:
//   - p: Bytes to append.
//
// Returns:
//   - int: Bytes accepted.
//   - error: Push failure.
func (w *Writer) Write(p []byte) (int, error) {
	if w == nil || w.logs == nil {
		return len(p), nil
	}

	w.buf = append(w.buf, p...)
	w.drain(false)

	if len(w.pending) == 0 {
		return len(p), nil
	}

	err := w.logs.Push(w.ctx, w.runID, w.caseID, w.streamName, w.pending)
	w.pending = w.pending[:0]

	return len(p), err
}

// Close flushes a trailing partial line.
//
// Returns:
//   - error: Push failure.
func (w *Writer) Close() error {
	if w == nil || w.logs == nil {
		return nil
	}

	w.drain(true)

	if len(w.pending) == 0 {
		return nil
	}

	err := w.logs.Push(w.ctx, w.runID, w.caseID, w.streamName, w.pending)
	w.pending = nil

	return err
}

// drain moves complete lines from buf into pending.
//
// Parameters:
//   - flushLast: When true, a trailing partial line is treated as complete.
func (w *Writer) drain(flushLast bool) {
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			if flushLast && len(w.buf) > 0 {
				w.pending = append(w.pending, Line{Time: time.Now().UTC(), Body: string(w.buf), Stream: w.streamName})
				w.buf = w.buf[:0]
			}

			return
		}

		w.pending = append(w.pending, Line{Time: time.Now().UTC(), Body: string(w.buf[:idx]), Stream: w.streamName})
		w.buf = w.buf[idx+1:]
	}
}

var _ io.WriteCloser = (*Writer)(nil)
