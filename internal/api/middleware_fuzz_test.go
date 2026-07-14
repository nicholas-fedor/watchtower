package api

import (
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// FuzzLogrusWriterWrite fuzzes the logrusWriter.Write method which processes
// byte slices by stripping trailing newlines. It tests that the method never
// panics and returns the correct byte count for any input.
func FuzzLogrusWriterWrite(f *testing.F) {
	f.Add([]byte("hello world\n"))
	f.Add([]byte("hello world"))
	f.Add([]byte(""))
	f.Add([]byte("\n"))
	f.Add([]byte("\n\n\n"))
	f.Add([]byte("test message  \n"))
	f.Add([]byte("unicode: \xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e\n"))
	f.Add([]byte("null\x00byte\n"))
	f.Add([]byte("very long string " + strings.Repeat("x", 1000) + "\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		logger := logrus.New()
		logger.SetOutput(io.Discard)

		w := &logrusWriter{logger: logger}

		n, err := w.Write(data)

		if n != len(data) {
			t.Errorf("Write() returned %d, want %d", n, len(data))
		}

		if err != nil {
			t.Errorf("Write() returned unexpected error: %v", err)
		}
	})
}
