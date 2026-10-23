package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/settings"
)

// unsetFlagEnv unsets every flag's environment variables for the test, so
// only command-line arguments configure the command. The original values
// are restored when the test ends.
func unsetFlagEnv(t *testing.T) {
	t.Helper()

	for _, flagSpec := range settings.AllSpecs() {
		for _, key := range flagSpec.EnvKeys {
			// t.Setenv restores the original value when the test ends.
			t.Setenv(key, "")
			require.NoError(t, os.Unsetenv(key))
		}
	}
}

// newTestRootCommand returns a new root command with every flag registered
// and args parsed, so tests never change the package's root command. Flag
// environment variables are unset and logging is limited to errors.
func newTestRootCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	unsetFlagEnv(t)

	root := NewRootCommand()

	settings.RegisterAll(root)

	require.NoError(t, root.ParseFlags(append([]string{"--log-level", "error"}, args...)))

	return root
}
