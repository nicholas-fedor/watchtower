package flagenv

import (
	"os"
	"testing"

	"github.com/nicholas-fedor/watchtower/internal/settings"
)

// Unset unsets every environment variable Watchtower's settings read, such as
// DOCKER_HOST and NO_COLOR. Other variables are left alone.
//
// It changes the whole process, so call it from TestMain before the tests
// run. A test that needs a variable sets it with t.Setenv.
func Unset() {
	for _, key := range settings.EnvKeys() {
		// os.Unsetenv fails only on platforms without unsetenv, where tests
		// cannot run isolated anyway.
		err := os.Unsetenv(key)
		if err != nil {
			panic(err)
		}
	}
}

// UnsetForTest unsets every environment variable Watchtower's settings read
// until tb ends, when each one gets back the value it had before the call. That
// also undoes the values the code under test writes. Like t.Setenv, it panics
// in a parallel test.
//
// Parameters:
//   - tb: The test the variables are unset for.
func UnsetForTest(tb testing.TB) {
	tb.Helper()

	for _, key := range settings.EnvKeys() {
		// tb.Setenv records the value to restore when the test ends.
		tb.Setenv(key, "")

		err := os.Unsetenv(key)
		if err != nil {
			tb.Fatalf("unset %s: %v", key, err)
		}
	}
}
