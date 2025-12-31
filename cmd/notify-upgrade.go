// Package cmd contains the watchtower (sub-)commands.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
)

// cleanupTimeout defines the duration after which the temporary notification file is removed.
// Using a constant avoids magic numbers and clarifies intent.
const cleanupTimeout = 5 * time.Minute

// Errors for notify-upgrade operations.
var (
	// errCreateTempFile indicates a failure to create a temporary file for notification URLs.
	errCreateTempFile = errors.New("failed to create output file")
	// errWriteTempFile indicates a failure to write notification URLs to the temporary file.
	errWriteTempFile = errors.New("failed to write to output file")
	// errSyncTempFile indicates a failure to sync the temporary file to disk.
	errSyncTempFile = errors.New("failed to sync output file")
)

// init registers the notify-upgrade command with the root command.
// It follows Cobra’s best practice of adding subcommands in an init function,
// avoiding global variables and ensuring proper command hierarchy setup.
func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "notify-upgrade",
		Short: "Upgrade legacy notification configuration to shoutrrr URLs",
		Long:  "Converts legacy Watchtower notification settings into shoutrrr URL format and writes them to a temporary file.",
		Run:   runNotifyUpgrade,
		Args:  cobra.NoArgs, // Enforces no positional arguments for this command.
	})
}

// runNotifyUpgrade executes the notify-upgrade command, handling errors gracefully.
// It wraps the main logic in runNotifyUpgradeE and logs any failures with logrus.
func runNotifyUpgrade(cmd *cobra.Command, args []string) {
	if err := runNotifyUpgradeE(cmd, args); err != nil {
		logrus.WithError(err).Error("Notification upgrade failed")
	}
}

// runNotifyUpgradeE performs the core logic for the `notify-upgrade` subcommand, converting legacy Watchtower notification
// settings into shoutrrr URL format and managing the lifecycle of a temporary file to store these URLs.
//
// Parameters:
//   - cmd: The *cobra.Command instance representing the `notify-upgrade` subcommand, providing access to parsed flags
//     such as notification types (e.g., --notifications) and other settings that influence notifier behavior.
//   - _: A slice of strings representing positional arguments, unused here as the command accepts no arguments (enforced
//     by cobra.NoArgs), included for compatibility with Cobra’s RunE signature.
//
// Returns:
//   - error: An error value if a critical operation fails (e.g., creating or writing to the temporary file), wrapped with
//     a static error (e.g., errCreateTempFile) and the underlying system error for context. Returns nil if the function
//     completes successfully, including cleanup, indicating the notification upgrade process ran without fatal issues.
//     Non-critical failures (e.g., file removal after timeout) are logged but do not result in an error return.
func runNotifyUpgradeE(cmd *cobra.Command, _ []string) error {
	// Process flag aliases to normalize inputs from environment variables or shorthand flags, ensuring consistent configuration.
	f := cmd.Flags()
	flags.ProcessFlagAliases(f)

	// Initialize the notifier with flag-derived settings, extracting legacy notification configurations into shoutrrr URLs.
	notifier := notifications.NewNotifier(cmd)
	urls := notifier.GetURLs()

	// Log the identified notification types (e.g., "email, slack") to inform the user of what configurations are being upgraded.
	logrus.WithField("notifiers", strings.Join(notifier.GetNames(), ", ")).
		Info("Found notification config(s)")

	// Create a temporary file in the root directory with a pattern that ensures uniqueness (e.g., "watchtower-notif-urls-123").
	// This file will store the generated URLs for user retrieval.
	outFile, err := os.CreateTemp("/", "watchtower-notif-urls-*")
	if err != nil {
		// Log the failure with the specific error and return a wrapped error to halt execution, as file creation is critical.
		logrus.WithError(err).Debug("Temporary file creation failed")

		return fmt.Errorf("%w: %w", errCreateTempFile, err)
	}
	// Ensure the file is closed after all operations, even on early returns, to prevent resource leaks.
	defer outFile.Close()

	// Log the file path where URLs will be written, providing the user with a concrete reference for later instructions.
	logrus.WithField("file", outFile.Name()).Info("Writing notification URLs")

	// Construct the environment variable string in the format "WATCHTOWER_NOTIFICATION_URL=url1 url2 ...", where multiple URLs
	// are space-separated as required by shoutrrr. This uses a strings.Builder for efficient string concatenation.
	urlBuilder := strings.Builder{}
	urlBuilder.WriteString("WATCHTOWER_NOTIFICATION_URL=")

	for i, u := range urls {
		if i != 0 {
			urlBuilder.WriteRune(' ') // Add a space between URLs for multi-notification configs.
		}

		urlBuilder.WriteString(u)
	}

	// Write the constructed string to the temporary file. This is a critical step, as the file’s purpose is to store this data.
	if _, err := fmt.Fprint(outFile, urlBuilder.String()); err != nil {
		logrus.WithError(err).
			WithField("file", outFile.Name()).
			Debug("Failed to write to temporary file")

		return fmt.Errorf("%w: %w", errWriteTempFile, err)
	}

	// Sync the file to disk to ensure the written data is persisted, preventing loss due to buffering or system crashes.
	if err := outFile.Sync(); err != nil {
		logrus.WithError(err).
			WithField("file", outFile.Name()).
			Debug("Failed to sync temporary file")

		return fmt.Errorf("%w: %w", errSyncTempFile, err)
	}

	// Attempt to retrieve the running container’s ID to provide precise instructions for copying the file from the container.
	// Use a placeholder ("<CONTAINER>") if this fails, ensuring the user still gets actionable guidance.
	containerID := "<CONTAINER>"

	if CurrentWatchtowerContainerID != "" {
		containerID = CurrentWatchtowerContainerID.ShortID() // Use the short ID (e.g., "abc123") for brevity in user instructions.
	}

	// Provide user instructions for retrieving the file, split into two log lines for clarity: a prompt and the exact command.
	logrus.Info(
		fmt.Sprintf(
			"To get the environment file, use: cp %s:%s ./watchtower-notifications.env",
			containerID,
			outFile.Name(),
		),
	)

	// Warn the user that the file is temporary and will be cleaned up, reinforcing the urgency to act within the timeout.
	logrus.Info("Note: This file will be removed in 5 minutes or when this container is stopped!")

	// Set up signal handling for cleanup.
	signalChannel := make(chan os.Signal, 1)
	timeoutChannel := make(chan struct{}, 1) // Buffer 1 to avoid blocking

	// Trigger cleanup after timeout
	time.AfterFunc(cleanupTimeout, func() {
		timeoutChannel <- struct{}{}
	})

	// Notify on interrupts
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	// Wait for a signal and perform cleanup.
	select {
	case <-timeoutChannel:
		logrus.Info("Timed out")
	case sig := <-signalChannel:
		logrus.WithField("signal", sig).Info("Stopping")
	}

	// Attempt to remove the temporary file. If this fails (e.g., due to permissions or file system issues), log a warning
	// rather than an error, as the command has completed its primary task, and leftover files are a minor concern.
	if err := os.Remove(outFile.Name()); err != nil {
		logrus.WithError(err).
			WithField("file", outFile.Name()).
			Warn("Failed to remove temporary file")
	} else {
		logrus.WithField("file", outFile.Name()).Info("Environment file removed")
	}

	// Return nil to indicate successful completion, even if non-critical operations (e.g., file removal) failed,
	// as the core task of generating and writing URLs has been accomplished.
	return nil
}
