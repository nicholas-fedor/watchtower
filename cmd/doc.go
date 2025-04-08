// Package cmd contains the command-line interface (CLI) definitions and execution logic for Watchtower.
// It provides the root command and subcommands to orchestrate container updates, notifications, and configuration upgrades.

// Key components:
//   - rootCmd: Root command for updates, API, and scheduling.
//   - notify-upgrade: Subcommand to convert legacy notifications to shoutrrr URLs.
//   - RunConfig: Struct for configuring execution.

// Usage example:
//   cmd.Execute() // Runs the CLI from main.go
//   // For notify-upgrade:
//   // watchtower notify-upgrade

// The package integrates with actions, container, notifications, and flags packages,
// using Cobra for CLI parsing and logrus for logging.
package cmd
