package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
)

// newCancelCommand defines the cancel subcommand.
//
// Returns:
//   - *cobra.Command: Cancel command.
func newCancelCommand() *cobra.Command {
	var runID string

	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel a queued or running sitting",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runID == "" {
				return wrapCmd("cancel", errRunRequired)
			}

			run, err := api.NewClient().Cancel(cmd.Context(), runID)
			if err != nil {
				return wrapCmd("cancel", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "e2e run %s %s\n", run.ID, run.Status)

			return nil
		},
	}

	cmd.Flags().StringVar(&runID, "run", "", "run UUID")

	return cmd
}
