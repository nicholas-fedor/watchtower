package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/settings"
	"github.com/nicholas-fedor/watchtower/internal/testutil/flagenv"
)

// newTestRootCommand returns a new root command with every flag registered
// and args parsed, so tests never change the package's root command. Logging
// is limited to errors. Flag environment variables are unset until the test
// ends, which also undoes the DOCKER_* variables its startup writes.
func newTestRootCommand(t *testing.T, args ...string) *cobra.Command {
	t.Helper()

	flagenv.UnsetForTest(t)

	root := NewRootCommand()

	settings.RegisterAll(root)

	require.NoError(t, root.ParseFlags(append([]string{"--log-level", "error"}, args...)))

	return root
}

// newExecuteCommand returns a new root command with every flag registered and
// a notify-upgrade subcommand, wired to proc as Execute wires the package's
// commands and set to parse args. Flag environment variables are unset until
// the test ends, which also undoes the DOCKER_* variables startup writes.
func newExecuteCommand(t *testing.T, proc *process, args ...string) *cobra.Command {
	t.Helper()

	flagenv.UnsetForTest(t)

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
