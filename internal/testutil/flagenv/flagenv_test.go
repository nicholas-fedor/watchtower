package flagenv

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/settings"
)

// readKeys are variables settings read. They cover a Docker connection flag,
// a Watchtower option, NO_COLOR and a cooldown platform variable that no flag
// binds.
var readKeys = []string{
	"DOCKER_HOST",
	"WATCHTOWER_POLL_INTERVAL",
	"NO_COLOR",
	"WATCHTOWER_COOLDOWN_PLATFORM_OS",
}

// keptValues are variables no setting reads. The WATCHTOWER_ one is like the
// variables that mark a re-executed test binary. Clearing such a marker makes
// the child run the parent test again, which spawns children without end.
var keptValues = map[string]string{
	"FLAGENV_TEST_UNRELATED":             "kept",
	"WATCHTOWER_CMD_PRE_RUN_EXIT_HELPER": "helper-case",
}

// setShellEnv sets every variable in readKeys and keptValues for the test, as
// a shell would.
func setShellEnv(t *testing.T) {
	t.Helper()

	for _, key := range readKeys {
		t.Setenv(key, "set-by-the-shell")
	}

	for key, value := range keptValues {
		t.Setenv(key, value)
	}
}

// assertIsolated checks that every variable in readKeys is unset and every
// variable in keptValues keeps its value.
func assertIsolated(t *testing.T) {
	t.Helper()

	for _, key := range readKeys {
		_, found := os.LookupEnv(key)
		assert.False(t, found, "%s is still set", key)
	}

	for key, value := range keptValues {
		assert.Equal(t, value, os.Getenv(key), "%s was changed", key)
	}
}

// TestUnset verifies that Unset removes the variables settings read and
// leaves other variables alone. Unset clears every settings variable for the
// whole process, so the test first records each one with t.Setenv, which
// restores it, or unsets it again, when the test ends. It uses t.Setenv, so
// it cannot run in parallel.
func TestUnset(t *testing.T) {
	for _, key := range settings.EnvKeys() {
		value, found := os.LookupEnv(key)
		t.Setenv(key, value)

		if !found {
			require.NoError(t, os.Unsetenv(key))
		}
	}

	setShellEnv(t)

	Unset()

	assertIsolated(t)
}

// TestUnsetForTest verifies that UnsetForTest removes the variables settings
// read for a subtest and leaves other variables alone. When the subtest ends,
// each variable gets back its earlier value, even one the subtest writes with
// os.Setenv the way cmd's startup does. Such a write has no cleanup of its
// own. The test uses t.Setenv, so it cannot run in parallel.
func TestUnsetForTest(t *testing.T) {
	setShellEnv(t)

	t.Run("isolated", func(t *testing.T) {
		UnsetForTest(t)

		assertIsolated(t)

		require.NoError(t, os.Setenv("DOCKER_HOST", "written-by-the-test"))
	})

	for _, key := range readKeys {
		assert.Equal(t, "set-by-the-shell", os.Getenv(key), "%s was not restored", key)
	}
}
