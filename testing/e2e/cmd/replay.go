package cmd

import (
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/run"
)

// newReplayCommand defines the replay subcommand for a stored case directory.
//
// Returns:
//   - *cobra.Command: Replay command.
func newReplayCommand() *cobra.Command {
	var caseDir string

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Re-run one case from an artifacts directory",
		RunE: func(_ *cobra.Command, _ []string) error {
			return wrapCmd("replay", run.Replay(caseDir))
		},
	}
	cmd.Flags().StringVar(&caseDir, "case", "", "artifacts/<run>/cases/<id>")

	return cmd
}
