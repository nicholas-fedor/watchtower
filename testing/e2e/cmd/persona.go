package cmd

import (
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/registry"
)

// newPersonaCommand defines the persona subcommand that serves fake registries.
//
// Returns:
//   - *cobra.Command: Persona command.
func newPersonaCommand() *cobra.Command {
	var (
		listen  string
		backend string
		persona string
	)

	cmd := &cobra.Command{
		Use:   "persona",
		Short: "Serve Hub/GHCR/LSCR registry dialects in front of distribution v2",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return wrapCmd("persona", registry.Serve(
				cmd.Context(),
				listen,
				backend,
				registry.Persona(persona),
			))
		},
	}
	cmd.Flags().StringVar(&listen, "listen", ":80", "proxy listen address")
	cmd.Flags().StringVar(&backend, "backend", "http://127.0.0.1:5000", "distribution registry URL")
	cmd.Flags().StringVar(&persona, "persona", "hub", "hub, ghcr, lscr, or private")

	return cmd
}
