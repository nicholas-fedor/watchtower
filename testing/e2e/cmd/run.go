package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/run"
)

// newRunCommand defines the run subcommand flags and prints sitting totals.
//
// Returns:
//   - *cobra.Command: Run command.
func newRunCommand() *cobra.Command {
	req := run.SittingRequest{
		Generator: "product",
		Seed:      1,
		Workers:   1,
	}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a slice of the cartesian product (hours-long full runs are expected)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			req.Keep = req.Keep || os.Getenv("WATCHTOWER_E2E_KEEP") == "1"
			if req.Workers < 1 {
				req.Workers = envInt("WATCHTOWER_E2E_WORKERS", 1)
			}

			result, err := run.Sitting(cmd.Context(), req)
			if err != nil {
				return wrapCmd("run", err)
			}

			_, _ = fmt.Fprintf(
				cmd.OutOrStdout(),
				"e2e run %s passed=%d failed=%d skipped=%d\n",
				result.RunID,
				result.Passed,
				result.Failed,
				result.Skipped,
			)

			return nil
		},
	}

	cmd.Flags().IntVar(&req.Workers, "workers", 1, "parallel DinD workers")
	cmd.Flags().StringVar(&req.Shard, "shard", "", "i/n shard of the product")
	cmd.Flags().IntVar(&req.Offset, "offset", 0, "skip this many selected cases")
	cmd.Flags().IntVar(&req.Limit, "limit", 0, "stop after N executed cases (0 = no cap)")
	cmd.Flags().StringVar(&req.Generator, "generator", "product", "product, random, or file")
	cmd.Flags().Int64Var(&req.Seed, "seed", 1, "random generator seed")
	cmd.Flags().StringVar(&req.Resume, "resume", "", "checkpoint.json path")
	cmd.Flags().StringVar(&req.Topic, "topic", "", "named development slice (see go run . list --topics)")
	cmd.Flags().StringVar(&req.Filter, "filter", "", "regex on case ID or factor values")
	cmd.Flags().BoolVar(&req.Keep, "keep", false, "keep successful case artifact dirs")
	cmd.Flags().StringVar(&req.FilePath, "file", "", "YAML cases for --generator file")

	return cmd
}
