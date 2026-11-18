package notify

import (
	"os"
	"testing"

	"github.com/nicholas-fedor/watchtower/internal/testutil/flagenv"
)

// TestMain unsets every environment variable a setting reads before any test
// runs, so a variable set in the shell, such as WATCHTOWER_NOTIFICATION_URL,
// cannot change the notifier a test builds from its command.
func TestMain(m *testing.M) {
	flagenv.Unset()

	os.Exit(m.Run())
}
