// Package cmd contains the command-line interface (CLI) definitions and execution logic for Watchtower.
// It provides the root command and subcommands to orchestrate container updates, notifications, and configuration upgrades.
//
// Key components:
//   - rootCmd: Root command for updates, API, and scheduling.
//   - notify-upgrade: Subcommand to convert legacy notifications to shoutrrr URLs.
//   - RunConfig: Struct for configuring execution.
//
// Usage examples:
//   - Run the CLI from main.go:
//     log := logging.New(os.Stderr, logging.InfoLevel)
//     cmd.Execute(log)
//   - Convert legacy notifications to shoutrrr URLs:
//     watchtower notify-upgrade
//
// The package integrates with actions, container, notifications, and flags packages,
// using Cobra for CLI parsing and github.com/rs/zerolog for process logging. The
// composition root constructs a *zerolog.Logger in main and passes it through Execute.
// There is no package-level global logger.
package cmd
