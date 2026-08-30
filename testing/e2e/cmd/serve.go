package cmd

import (
	"cmp"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/infra"
	"github.com/nicholas-fedor/watchtower/testing/e2e/server"
)

// newServeCommand defines the serve subcommand that starts the control plane.
//
// Returns:
//   - *cobra.Command: Serve command.
func newServeCommand() *cobra.Command {
	var listen, token string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the e2e control plane (API, dashboard, queue)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			addr := cmp.Or(listen, infra.FromEnv().Listen)

			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "e2e control plane listening on http://%s  (dashboard /  api /v1  docs /v1/docs)\n", addr)

			return wrapCmd("serve", server.Listen(ctx, addr, token))
		},
	}

	env := infra.FromEnv()
	cmd.Flags().StringVar(&listen, "listen", env.Listen, "bind address")
	cmd.Flags().StringVar(&token, "token", env.Token, "optional bearer token")

	return cmd
}
