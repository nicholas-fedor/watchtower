package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
)

// newStatusCommand defines the status subcommand that prints sittings.
//
// Returns:
//   - *cobra.Command: Status command.
func newStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status [run-id]",
		Short: "Print sitting status from the control plane",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli := api.NewClient()
			if !cli.Healthy(cmd.Context()) {
				return wrapCmd("status", fmt.Errorf("%w at %s", errServeUnreachable, cli.Base()))
			}

			if len(args) == 1 {
				run, err := cli.GetRun(cmd.Context(), args[0])
				if err != nil {
					return wrapCmd("status", err)
				}

				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"%s  %s  passed=%d failed=%d skipped=%d  current=%v\n",
					run.ID, run.Status, run.Passed, run.Failed, run.Skipped, run.CurrentIDs)

				return nil
			}

			runs, err := cli.ListRuns(cmd.Context())
			if err != nil {
				return wrapCmd("status", err)
			}

			for _, run := range runs {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %-12s  %s  p=%d f=%d s=%d\n",
					run.ID, run.Status, run.Label, run.Passed, run.Failed, run.Skipped)
			}

			return nil
		},
	}
}
