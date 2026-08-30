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

// newRootCommand builds the Cobra tree for serve, run, status, cases, logs, cancel, list, doctor, replay, and persona.
//
// Returns:
//   - *cobra.Command: Root command.
func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "e2e",
		Short: "Watchtower black-box e2e control plane (DinD, Postgres, Loki)",
	}
	root.AddCommand(
		newRunCommand(),
		newServeCommand(),
		newStatusCommand(),
		newCasesCommand(),
		newLogsCommand(),
		newCancelCommand(),
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

// envBool reports whether key is the string "1".
//
// Parameters:
//   - key: Environment variable name.
//
// Returns:
//   - bool: True when the variable is "1".
func envBool(key string) bool {
	return os.Getenv(key) == "1"
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
