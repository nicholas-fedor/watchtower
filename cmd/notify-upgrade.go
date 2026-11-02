package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/observability/notify/upgrade"
	"github.com/nicholas-fedor/watchtower/internal/settings"
)

// cleanupTimeout defines the duration after which the temporary notification file is removed.
// Using a constant avoids magic numbers and clarifies intent.
const cleanupTimeout = 5 * time.Minute

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
//   - error: Non-nil if the settings cannot be loaded or turned into notification URLs. Non-nil also if the
//     environment file cannot be written. A failure to remove the file afterwards is only logged.
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

	// Write the file to the working directory, where the instructions below tell the user to copy it from.
	fileName, err := upgrade.WriteEnvFile(log, ".", cfg.Notify)
	if err != nil {
		return log, err //nolint:wrapcheck // WriteEnvFile's errors already name the step that failed.
	}

	// Attempt to retrieve the running container's ID to provide precise instructions for copying the file from the container.
	// Use a placeholder ("<CONTAINER>") if this fails, ensuring the user still gets actionable guidance.
	containerID := "<CONTAINER>"

	cid, err := containerClient.GetContainerIDFromMountinfo(log, processFs)
	if err == nil && cid != "" {
		containerID = cid.ShortID()
	} else {
		cid, err = containerClient.GetContainerIDFromCgroupFile(log, processFs)
		if err == nil && cid != "" {
			containerID = cid.ShortID()
		}
	}

	// Tell the user how to copy the file out of the container.
	log.Info().
		Msg(fmt.Sprintf(
			"To get the environment file, use: docker cp %s:%s ./watchtower-notifications.env",
			containerID,
			fileName,
		))

	// Warn the user that the file is temporary and will be cleaned up, reinforcing the urgency to act within the timeout.
	log.Info().Msg(fmt.Sprintf(
		"Note: This file will be removed in %d minutes or when this container is stopped!",
		int(cleanupTimeout/time.Minute),
	))

	// Wait for the timeout or an interrupt, then remove the file.
	timer := time.NewTimer(cleanupTimeout)
	defer timer.Stop()

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	defer signal.Stop(signalChannel)

	select {
	case <-timer.C:
		log.Info().Msg("Timed out")
	case sig := <-signalChannel:
		log.Info().
			Str("signal", sig.String()).
			Msg("Stopping")
	}

	upgrade.RemoveEnvFile(log, fileName)

	return log, nil
}
