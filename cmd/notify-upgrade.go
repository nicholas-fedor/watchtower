package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/settings"
	"github.com/nicholas-fedor/watchtower/pkg/notifications"
	"github.com/nicholas-fedor/watchtower/pkg/types"
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
	// errCloseTempFile indicates a failure to close the temporary file after writing it.
	errCloseTempFile = errors.New("failed to close output file")
)

// notifyUpgradeCmd is the notify-upgrade subcommand. Execute wires its RunE to
// the process that runs it.
var notifyUpgradeCmd = &cobra.Command{
	Use:   "notify-upgrade",
	Short: "Upgrade legacy notification configuration to shoutrrr URLs",
	Long:  "Converts legacy Watchtower notification settings into shoutrrr URL format and writes them to a temporary file.",
	Args:  cobra.NoArgs, // Enforces no positional arguments for this command.
}

// init registers the notify-upgrade command with the root command.
func init() {
	rootCmd.AddCommand(notifyUpgradeCmd)
}

// runNotifyUpgrade executes the notify-upgrade command.
//
// The process logger is replaced by the logger configured from the command's
// flags, so Execute reports a failure with it.
//
// Parameters:
//   - cmd: The notify-upgrade command, providing access to parsed flags.
//   - args: Positional arguments, none for this command.
//
// Returns:
//   - error: An [exitError] when the upgrade fails.
func (p *process) runNotifyUpgrade(cmd *cobra.Command, args []string) error {
	// Execute reports the errors returned from here on. Flag and argument
	// errors come earlier, so Cobra still prints them with the usage text.
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var err error

	p.log, err = runNotifyUpgradeE(cmd, args, p.log)
	if err != nil {
		return &exitError{err: err, msg: "Notification upgrade failed", code: 1}
	}

	return nil
}

// runNotifyUpgradeE performs the core logic for the `notify-upgrade` subcommand, converting legacy Watchtower notification
// settings into shoutrrr URL format and managing the lifecycle of a temporary file to store these URLs.
//
// Parameters:
//   - cmd: The *cobra.Command instance representing the `notify-upgrade` subcommand, providing access to parsed flags
//     such as notification types (e.g., --notifications) and other settings that influence notifier behavior.
//   - _: A slice of strings representing positional arguments, unused here as the command accepts no arguments (enforced
//     by cobra.NoArgs), included for compatibility with Cobra's RunE signature.
//   - log: Process logger, reconfigured from --log-format / --log-level so subcommand output honors those flags.
//
// Returns:
//   - *zerolog.Logger: Logger after SetupLogging (format/level applied) for outer error reporting.
//   - error: An error value if a critical operation fails (e.g., creating or writing to the temporary file), wrapped with
//     a static error (e.g., errCreateTempFile) and the underlying system error for context. Returns nil if the function
//     completes successfully, including cleanup, indicating the notification upgrade process ran without fatal issues.
//     Non-critical failures (e.g., file removal after timeout) are logged but do not result in an error return.
func runNotifyUpgradeE(cmd *cobra.Command, _ []string, log *zerolog.Logger) (*zerolog.Logger, error) {
	// Process flag aliases and expand secrets before resolving configuration.
	// Use Root().PersistentFlags so env/alias bridging matches settings.Load's bind source across subcommands.
	flagSet := cmd.Root().PersistentFlags()

	err := settings.ApplyEnvToFlags(flagSet, settings.AllSpecs())
	if err != nil {
		return log, fmt.Errorf("apply environment configuration: %w", err)
	}

	// Format before aliases so their errors use the selected --log-format.
	log, err = settings.SetupLogging(log, flagSet)
	if err != nil {
		return log, fmt.Errorf("setup logging: %w", err)
	}

	err = settings.ProcessFlagAliases(log, flagSet)
	if err != nil {
		return log, fmt.Errorf("process flag aliases: %w", err)
	}

	// Re-apply after aliases so debug/trace forced log-level values take effect.
	log, err = settings.SetupLogging(log, flagSet)
	if err != nil {
		return log, fmt.Errorf("setup logging: %w", err)
	}

	err = settings.GetSecretsFromFiles(log, cmd.Root())
	if err != nil {
		return log, fmt.Errorf("load secrets from files: %w", err)
	}

	cfg, loadErr := settings.Load(log, cmd.Root(), nil)
	if loadErr != nil {
		return log, fmt.Errorf("load configuration: %w", loadErr)
	}

	urls, buildErr := notifications.BuildURLs(log, cfg.Notify)
	if buildErr != nil {
		return log, fmt.Errorf("build notification URLs: %w", buildErr)
	}

	// Log the identified notification types (e.g., "email, slack") to inform the user of what configurations are being upgraded.
	log.Info().
		Str("notifiers", strings.Join(cfg.Notify.LegacyTypes, ", ")).
		Msg("Found notification config(s)")

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

	// Write the file to the working directory, where the instructions below tell the user to copy it from.
	fileName, err := writeNotificationEnvFile(log, ".", urlBuilder.String(), (*os.File).Sync)
	if err != nil {
		return log, err
	}

	// Attempt to retrieve the running container's ID to provide precise instructions for copying the file from the container.
	// Use a placeholder ("<CONTAINER>") if this fails, ensuring the user still gets actionable guidance.
	containerID := "<CONTAINER>"

	var cid types.ContainerID

	cid, err = containerClient.GetContainerIDFromMountinfo(log, processFs)
	if err == nil && cid != "" {
		containerID = cid.ShortID()
	} else {
		cid, err = containerClient.GetContainerIDFromCgroupFile(log, processFs)
		if err == nil && cid != "" {
			containerID = cid.ShortID()
		}
	}

	// Provide user instructions for retrieving the file, split into two log lines for clarity: a prompt and the exact command.
	log.Info().
		Msg(fmt.Sprintf(
			"To get the environment file, use: docker cp %s:%s ./watchtower-notifications.env",
			containerID,
			fileName,
		))

	// Warn the user that the file is temporary and will be cleaned up, reinforcing the urgency to act within the timeout.
	log.Info().Msg("Note: This file will be removed in 5 minutes or when this container is stopped!")

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
		log.Info().Msg("Timed out")
	case sig := <-signalChannel:
		log.Info().
			Str("signal", sig.String()).
			Msg("Stopping")
	}

	// Attempt to remove the temporary file. If this fails (e.g., due to permissions or file system issues), log a warning
	// rather than an error, as the command has completed its primary task, and leftover files are a minor concern.
	err = os.Remove(fileName)
	if err != nil {
		log.Warn().
			Err(err).
			Str("file", fileName).
			Msg("Failed to remove temporary file")
	} else {
		log.Info().
			Str("file", fileName).
			Msg("Environment file removed")
	}

	// Return nil to indicate successful completion, even if non-critical operations (e.g., file removal) failed,
	// as the core task of generating and writing URLs has been accomplished.
	return log, nil
}

// writeNotificationEnvFile writes the environment line to a new temporary file in dir.
//
// The file holds credential-bearing notification URLs, so a file that cannot be written, synced, and closed is
// removed before the error is returned.
//
// Parameters:
//   - log: Logger for the file path and failures.
//   - dir: Directory in which to create the file.
//   - line: Environment line to write, without the trailing newline.
//   - syncFile: Flushes the file to disk. Production passes (*os.File).Sync.
//
// Returns:
//   - string: Path of the written and closed file.
//   - error: errCreateTempFile, errWriteTempFile, errSyncTempFile, or errCloseTempFile wrapping the cause.
func writeNotificationEnvFile(
	log *zerolog.Logger,
	dir, line string,
	syncFile func(*os.File) error,
) (string, error) {
	outFile, err := os.CreateTemp(dir, "watchtower-notif-urls-*")
	if err != nil {
		log.Debug().Err(err).Msg("Temporary file creation failed")

		return "", fmt.Errorf("%w: %w", errCreateTempFile, err)
	}

	log.Info().
		Str("file", outFile.Name()).
		Msg("Writing notification URLs")

	_, err = fmt.Fprintln(outFile, line)
	if err != nil {
		err = fmt.Errorf("%w: %w", errWriteTempFile, err)
	} else {
		err = syncFile(outFile)
		if err != nil {
			err = fmt.Errorf("%w: %w", errSyncTempFile, err)
		}
	}

	// Close the file even after a failure, because Windows does not allow removing an open file.
	closeErr := outFile.Close()
	if err == nil && closeErr != nil {
		err = fmt.Errorf("%w: %w", errCloseTempFile, closeErr)
	}

	if err == nil {
		return outFile.Name(), nil
	}

	log.Debug().
		Err(err).
		Str("file", outFile.Name()).
		Msg("Failed to write temporary file")

	removeErr := os.Remove(outFile.Name())
	if removeErr != nil {
		log.Warn().
			Err(removeErr).
			Str("file", outFile.Name()).
			Msg("Failed to remove temporary file")
	}

	return "", err
}
