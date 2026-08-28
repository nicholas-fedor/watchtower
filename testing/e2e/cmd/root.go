package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

// Execute runs the e2e CLI and exits non-zero on error.
//
// It constructs the root Cobra command and delegates flag parsing and subcommand
// dispatch. A non-nil error from Execute becomes process exit status 1.
func Execute() {
	err := newRootCommand().Execute()
	if err != nil {
		os.Exit(1)
	}
}

// newRootCommand builds the Cobra tree for run, list, doctor, replay, and persona.
//
// Returns:
//   - *cobra.Command: Root command.
func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "e2e",
		Short: "Watchtower black-box e2e engine (Testcontainers DinD, cartesian product)",
	}
	root.AddCommand(
		newRunCommand(),
		newListCommand(),
		newDoctorCommand(),
		newReplayCommand(),
		newPersonaCommand(),
	)

	return root
}

// envInt parses an integer environment variable, or returns fallback.
//
// Parameters:
//   - key: Environment variable name.
//   - fallback: Value when the variable is empty or not an integer.
//
// Returns:
//   - int: Parsed value or fallback.
func envInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return parsed
}

// wrapCmd prefixes a backend error with the subcommand name.
//
// Parameters:
//   - op: Subcommand name such as run or doctor.
//   - err: Backend error, or nil.
//
// Returns:
//   - error: Wrapped error, or nil when err is nil.
func wrapCmd(op string, err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", op, err)
}
