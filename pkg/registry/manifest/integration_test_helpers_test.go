package manifest_test

import (
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/testutil/logtest"
)

func testLog() *zerolog.Logger {
	return logtest.Nop()
}
