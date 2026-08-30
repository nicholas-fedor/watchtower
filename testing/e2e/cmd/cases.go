package cmd

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
)

// newCasesCommand defines the cases subcommand that lists sitting cases.
//
// Returns:
//   - *cobra.Command: Cases command.
func newCasesCommand() *cobra.Command {
	var runID, status, query string

	cmd := &cobra.Command{
		Use:   "cases",
		Short: "List cases for a sitting",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runID == "" {
				return wrapCmd("cases", errRunRequired)
			}

			cases, total, err := api.NewClient().ListCases(cmd.Context(), runID, status, query)
			if err != nil {
				return wrapCmd("cases", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "total=%d\n", total)

			return json.MarshalWrite(cmd.OutOrStdout(), cases, jsontext.WithIndent("  "))
		},
	}

	cmd.Flags().StringVar(&runID, "run", "", "run UUID")
	cmd.Flags().StringVar(&status, "status", "", "pass, fail, skip, running, interrupted")
	cmd.Flags().StringVar(&query, "filter", "", "substring on case id or factors")

	return cmd
}
