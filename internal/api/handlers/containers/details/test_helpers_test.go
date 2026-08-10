package details

import (
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/logging"
)

func testLogger() *zerolog.Logger { return logging.NopLogger() }
