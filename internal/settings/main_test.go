package settings_test

import (
	"os"
	"testing"

	"github.com/nicholas-fedor/watchtower/internal/testutil/flagenv"
)

// TestMain unsets every environment variable a setting reads before any test
// runs, so a variable set in the shell, such as DOCKER_HOST, cannot change
// the configuration a test loads.
func TestMain(m *testing.M) {
	flagenv.Unset()

	os.Exit(m.Run())
}
