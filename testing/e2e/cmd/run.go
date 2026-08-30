package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
	"github.com/nicholas-fedor/watchtower/testing/e2e/server"
)

// newRunCommand defines the run subcommand that queues a sitting.
//
// Returns:
//   - *cobra.Command: Run command.
func newRunCommand() *cobra.Command {
	spec := api.Spec{
		Generator: "product",
		Seed:      1,
	}

	var resume string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Queue a sitting on the control plane (embeds serve if needed)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			spec.Keep = spec.Keep || envBool("WATCHTOWER_E2E_KEEP")
			if spec.Workers < 1 {
				spec.Workers = envInt("WATCHTOWER_E2E_WORKERS", 0)
			}

			ensureErr := server.Ensure(cmd.Context())
			if ensureErr != nil {
				return wrapCmd("run", ensureErr)
			}

			cli := api.NewClient()

			var (
				run api.Run
				err error
			)

			if resume != "" {
				run, err = cli.Resume(cmd.Context(), resume)
			} else {
				run, err = cli.CreateRun(cmd.Context(), spec)
			}

			if err != nil {
				return wrapCmd("run", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "e2e run %s queued  dashboard %s/runs/%s\n",
				run.ID, cli.Base(), run.ID)

			done, waitErr := cli.Wait(cmd.Context(), run.ID)
			if waitErr != nil {
				return wrapCmd("run", waitErr)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "e2e run %s passed=%d failed=%d skipped=%d\n",
				done.ID, done.Passed, done.Failed, done.Skipped)

			switch done.Status {
			case api.RunCompleted:
				if done.Failed > 0 {
					return wrapCmd("run", fmt.Errorf("%w: %d", errRunCasesFailed, done.Failed))
				}

				return nil
			case api.RunFailed:
				return wrapCmd("run", fmt.Errorf("%w: %s", errRunHarness, done.Error))
			default:
				return wrapCmd("run", fmt.Errorf("%w: %s", errRunStopped, done.Status))
			}
		},
	}

	cmd.Flags().IntVar(&spec.Workers, "workers", 0, "parallel DinD workers (0 = auto from host)")
	cmd.Flags().StringVar(&spec.Shard, "shard", "", "i/n shard of the product")
	cmd.Flags().IntVar(&spec.Offset, "offset", 0, "skip this many selected cases")
	cmd.Flags().IntVar(&spec.Limit, "limit", 0, "stop after N executed cases (0 = no cap)")
	cmd.Flags().StringVar(&spec.Generator, "generator", "product", "product, random, or file")
	cmd.Flags().Int64Var(&spec.Seed, "seed", 1, "random generator seed")
	cmd.Flags().StringVar(&resume, "resume", "", "run UUID to resume")
	cmd.Flags().StringVar(&spec.Topic, "topic", "", "named development slice (see go run . list --topics)")
	cmd.Flags().StringVar(&spec.Filter, "filter", "", "regex on case ID or factor values")
	cmd.Flags().BoolVar(&spec.Keep, "keep", false, "keep extra per-case documents")
	cmd.Flags().StringVar(&spec.FilePath, "file", "", "YAML cases for --generator file")

	return cmd
}
