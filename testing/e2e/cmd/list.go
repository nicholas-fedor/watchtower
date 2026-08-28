package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// newListCommand defines the list subcommand that prints topics and cardinality.
//
// Returns:
//   - *cobra.Command: List command.
func newListCommand() *cobra.Command {
	var (
		dump      bool
		generator string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Print topics, product cardinality, and factor table (no Docker)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			_, _ = fmt.Fprintln(out, "topics (development: go run . run --topic NAME --limit 20 --keep)")
			for _, topic := range engine.Topics() {
				_, _ = fmt.Fprintf(out, "  %-12s %s\n", topic.Name, topic.Summary)
			}

			_, _ = fmt.Fprintln(out)

			inv := engine.BuildInventory(generator, dump)
			_, _ = fmt.Fprintf(out, "generator=%s cardinality=%s factors=%d\n", inv.Generator, inv.Cardinality, inv.FactorCount)

			if inv.FirstID != "" {
				_, _ = fmt.Fprintf(out, "first=%s\n", inv.FirstID)
			}

			if inv.LastID != "" {
				_, _ = fmt.Fprintf(out, "last=%s\n", inv.LastID)
			}

			for _, factor := range inv.Factors {
				_, _ = fmt.Fprintf(out, "%s\t%d\t%s\n", factor.Name, factor.Count, factor.Levels)
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&dump, "dump-factors", false, "print factor names and levels")
	cmd.Flags().StringVar(&generator, "generator", "product", "product, random, or file")

	return cmd
}
