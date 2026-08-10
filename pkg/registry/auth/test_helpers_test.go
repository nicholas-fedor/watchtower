package auth

import (
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/logging"
)

func testLog() *zerolog.Logger {
	return logging.NopLogger()
}
