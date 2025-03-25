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

	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/flags"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
)

// errCreateTempFile is a static error for failures in creating temporary files.
// It allows consistent error checking and wrapping with additional context.
var errCreateTempFile = errors.New("failed to create output file")

// cleanupTimeout defines the duration after which the temporary notification file is removed.
// Using a constant avoids magic numbers and clarifies intent.
const cleanupTimeout = 5 * time.Minute

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
// It wraps the main logic in runNotifyUpgradeE and logs any failures to stderr.
func runNotifyUpgrade(cmd *cobra.Command, args []string) {
	if err := runNotifyUpgradeE(cmd, args); err != nil {
		logf("Notification upgrade failed: %v", err)
	}
}

// runNotifyUpgradeE performs the core logic of converting notification settings to URLs.
// It processes flags, generates URLs, writes them to a temp file, and cleans up after a timeout or signal.
// Returns an error if any step fails, allowing the caller to handle it.
func runNotifyUpgradeE(cmd *cobra.Command, _ []string) error {
	f := cmd.Flags()
	flags.ProcessFlagAliases(f)

	// Initialize the notifier with command flags to extract notification settings.
	notifier := notifications.NewNotifier(cmd)
	urls := notifier.GetURLs()

	logf("Found notification configurations for: %v", strings.Join(notifier.GetNames(), ", "))

	// Create a temporary file for storing the notification URLs.
	outFile, err := os.CreateTemp("/", "watchtower-notif-urls-*")
	if err != nil {
		return fmt.Errorf("%w: %w", errCreateTempFile, err)
	}

	logf("Writing notification URLs to %v", outFile.Name())
	logf("")

	// Build the environment variable string with all URLs.
	urlBuilder := strings.Builder{}
	urlBuilder.WriteString("WATCHTOWER_NOTIFICATION_URL=")

	for i, u := range urls {
		if i != 0 {
			urlBuilder.WriteRune(' ') // Space-separate multiple URLs.
		}

		urlBuilder.WriteString(u)
	}

	// Write the constructed string to the temp file.
	_, err = fmt.Fprint(outFile, urlBuilder.String())
	tryOrLog(err, "Failed to write to output file")

	tryOrLog(outFile.Sync(), "Failed to sync output file")
	tryOrLog(outFile.Close(), "Failed to close output file")

	// Determine the container ID for user instructions.
	containerID := "<CONTAINER>"
	cid, err := container.GetRunningContainerID()
	tryOrLog(err, "Failed to get running container ID")

	if cid != "" {
		containerID = cid.ShortID()
	}

	logf("To get the environment file, use:")
	logf("cp %v:%v ./watchtower-notifications.env", containerID, outFile.Name())
	logf("")
	logf("Note: This file will be removed in 5 minutes or when this container is stopped!")

	// Set up signal handling for cleanup.
	signalChannel := make(chan os.Signal, 1)

	// Trigger cleanup after the timeout period.
	time.AfterFunc(cleanupTimeout, func() {
		signalChannel <- syscall.SIGALRM
	})

	signal.Notify(signalChannel, os.Interrupt)
	signal.Notify(signalChannel, syscall.SIGTERM)

	// Wait for a signal and perform cleanup.
	switch <-signalChannel {
	case syscall.SIGALRM:
		logf("Timed out!")
	case os.Interrupt, syscall.SIGTERM:
		logf("Stopping...")
	default:
	}

	if err := os.Remove(outFile.Name()); err != nil {
		logf(
			"Failed to remove file, it may still be present in the container image! Error: %v",
			err,
		)
	} else {
		logf("Environment file has been removed.")
	}

	return nil
}

// tryOrLog logs an error with a message if the error is non-nil.
// It simplifies error handling for non-critical operations by reporting to stderr.
func tryOrLog(err error, message string) {
	if err != nil {
		logf("%v: %v\n", message, err)
	}
}

// logf writes formatted messages to stderr.
// It ensures consistent logging output for user-facing messages.
func logf(format string, v ...any) {
	fmt.Fprintln(os.Stderr, fmt.Sprintf(format, v...))
}
