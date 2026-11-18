package cmd

import (
	"os"
	"os/signal"
	"testing"

	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	"github.com/nicholas-fedor/watchtower/internal/testutil/flagenv"
)

// TestMain prepares the process before any test runs. It unsets every
// environment variable a setting reads, so a variable set in the shell cannot
// change a command a test builds. It also starts the process-wide goroutines
// that the code under test starts lazily.
//
// The os/signal delivery goroutine starts on the first signal.Notify call, and
// the metrics goroutine on the first metrics.Default call. When either first
// call comes from a test inside a synctest bubble, such as one that runs the
// scheduler, the goroutine belongs to that bubble. A later signal.Notify from
// outside the bubble then aborts the test binary, and the metrics goroutine
// leaves the bubble deadlocked when its test ends. Starting both here keeps
// them outside every bubble, whatever order the tests run in.
func TestMain(m *testing.M) {
	flagenv.Unset()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	signal.Stop(signals)

	metrics.Default()

	os.Exit(m.Run())
}
