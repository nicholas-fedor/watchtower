package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

// newDoctorCommand defines the doctor subcommand that pings DinD and flag coverage.
//
// Returns:
//   - *cobra.Command: Doctor command.
func newDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "DinD ping, no-egress, and --help versus Model() flag coverage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			missing := engine.UncoveredFlags()
			for _, name := range missing {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "doctor: Model() does not cover flag --%s\n", name)
			}

			info, err := docker.ProbeDaemon(cmd.Context())
			if err != nil {
				return wrapCmd("doctor", err)
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "dind docker version %s host %s\n", info.Version, info.Host)

			if len(missing) > 0 {
				return wrapCmd("doctor", fmt.Errorf("%w: %d", engine.ErrFlagsUncovered, len(missing)))
			}

			return nil
		},
	}
}
