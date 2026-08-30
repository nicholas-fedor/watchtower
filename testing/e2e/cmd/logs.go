package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
)

// newLogsCommand defines the logs subcommand that prints case streams.
//
// Returns:
//   - *cobra.Command: Logs command.
func newLogsCommand() *cobra.Command {
	var runID, caseID, streamName string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print Watchtower logs for one case",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runID == "" || caseID == "" {
				return wrapCmd("logs", errLogsRequired)
			}

			lines, err := api.NewClient().Logs(cmd.Context(), runID, caseID, streamName)
			if err != nil {
				return wrapCmd("logs", err)
			}

			for _, line := range lines {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", line.Stream, line.Body)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&runID, "run", "", "run UUID")
	cmd.Flags().StringVar(&caseID, "case", "", "case id")
	cmd.Flags().StringVar(&streamName, "stream", "", "stdout or stderr")

	return cmd
}
