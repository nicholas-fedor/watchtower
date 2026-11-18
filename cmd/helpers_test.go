package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/settings"
	"github.com/nicholas-fedor/watchtower/internal/testutil/flagenv"
)

// isolateStartupEnv keeps the environment variables a test's startup writes
// from reaching the next test. Every settings variable is unset until t ends.
// DOCKER_TLS keeps its value and gets it back when t ends. Settings do not
// read it, but startup writes it for the build tools Compose starts.
func isolateStartupEnv(t *testing.T) {
	t.Helper()

	flagenv.UnsetForTest(t)

	value, found := os.LookupEnv("DOCKER_TLS")
	t.Setenv("DOCKER_TLS", value)

	if !found {
		require.NoError(t, os.Unsetenv("DOCKER_TLS"))
	}
}

// newTestRootCommand returns a new root command with every flag registered
// and args parsed, so tests never change the package's root command. Its
// context is the test's context. Logging is limited to errors. The DOCKER_*
// variables its startup writes are undone when the test ends.
func newTestRootCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	isolateStartupEnv(t)

	root := NewRootCommand()
	root.SetContext(t.Context())

	settings.RegisterAll(root)

	require.NoError(t, root.ParseFlags(append([]string{"--log-level", "error"}, args...)))

	return root
}

// newExecuteCommand returns a new root command with every flag registered and
// a notify-upgrade subcommand, wired to proc as Execute wires the package's
// commands and set to parse args. The DOCKER_* variables startup writes are
// undone when the test ends.
func newExecuteCommand(t *testing.T, proc *process, args ...string) *cobra.Command {
	t.Helper()

	isolateStartupEnv(t)

	root := NewRootCommand()
	settings.RegisterAll(root)
	root.PreRunE = proc.preRun
	root.RunE = proc.run
	root.AddCommand(&cobra.Command{
		Use:  "notify-upgrade",
		Args: cobra.NoArgs,
		RunE: proc.runNotifyUpgrade,
	})
	root.SetArgs(args)

	return root
}
