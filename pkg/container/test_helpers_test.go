package container

import (
	"time"

	"github.com/onsi/gomega/gbytes"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/logging"
)

// testLog returns a discarded zerolog logger for tests that do not assert on output.
func testLog() *zerolog.Logger {
	return logging.NopLogger()
}

// captureLog returns a logfmt *zerolog.Logger writing to a gbytes.Buffer for gomega.Say assertions.
func captureLog(level zerolog.Level) (*zerolog.Logger, *gbytes.Buffer) {
	buf := gbytes.NewBuffer()
	w := logging.LogfmtWriter(buf)
	// Preserve timestamp style used by production logfmt.
	w.TimeFormat = time.RFC3339
	l := zerolog.New(w).Level(level).With().Timestamp().Logger()

	return &l, buf
}
