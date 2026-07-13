package api

import (
	"io"

	"github.com/sirupsen/logrus"
)

// logrusWriter implements io.Writer, writing each line to the given logrus
// logger at Info level. It is passed as the Fiber logger middleware's Stream
// so that HTTP request/response logs go through the same logrus pipeline as
// the rest of Watchtower.
type logrusWriter struct {
	logger *logrus.Logger
}

// Write implements io.Writer, writing each line to the logrus logger at Info level.
func (w *logrusWriter) Write(bytes []byte) (int, error) {
	msg := string(bytes)
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}

	w.logger.WithField("notify", "no").Info(msg)

	return len(bytes), nil
}

var _ io.Writer = (*logrusWriter)(nil)
