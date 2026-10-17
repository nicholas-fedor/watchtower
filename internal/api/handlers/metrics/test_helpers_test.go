package metrics

import (
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/testutil/logtest"
)

func testLogger() *zerolog.Logger { return logtest.Nop() }
