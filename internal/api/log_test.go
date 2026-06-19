package api

import (
	"bytes"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func Test_logrusWriter_Write(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantN   int
		wantErr bool
		wantLog string
	}{
		{
			name:    "plain message with newline",
			input:   []byte("hello world\n"),
			wantN:   12,
			wantLog: "hello world",
		},
		{
			name:    "plain message without newline",
			input:   []byte("hello world"),
			wantN:   11,
			wantLog: "hello world",
		},
		{
			name:    "empty input",
			input:   []byte{},
			wantN:   0,
			wantLog: "",
		},
		{
			name:    "only newline",
			input:   []byte("\n"),
			wantN:   1,
			wantLog: "",
		},
		{
			name:    "message with trailing whitespace",
			input:   []byte("test message  \n"),
			wantN:   15,
			wantLog: "test message  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()

			var buf bytes.Buffer
			logger.SetOutput(&buf)
			logger.SetLevel(logrus.InfoLevel)

			w := &logrusWriter{logger: logger}
			got, err := w.Write(tt.input)

			assert.Equal(t, tt.wantN, got)
			assert.Equal(t, tt.wantErr, err != nil)

			if tt.wantLog != "" {
				assert.Contains(t, buf.String(), tt.wantLog)
			}
		})
	}
}
