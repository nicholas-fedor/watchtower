package cmd

import (
	"context"
	"fmt"
	"time"

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
// It writes the legacy notification settings as Shoutrrr URLs to an
// environment file in the working directory. The file is removed after 5
// minutes or when the command's context ends.
//
// The process logger is replaced by the logger configured from the command's
// flags, so Execute reports a failure with it.
//
// Parameters:
//   - cmd: The notify-upgrade command, whose root holds the settings flags.
//   - _: Positional arguments, none for this command.
//
// Returns:
//   - error: An [exitError] when the settings cannot be loaded or turned into
//     notification URLs or when the environment file cannot be written. A
//     failure to remove the file afterwards is only logged.
func (p *process) runNotifyUpgrade(cmd *cobra.Command, _ []string) error {
	// Execute reports the errors returned from here on. Flag and argument
	// errors come earlier, so Cobra still prints them with the usage text.
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	const failure = "Notification upgrade failed"

	// The settings live on the root command's flags.
	cfg, log, err := settings.Load(p.log, cmd.Root(), nil)
	p.log = log

	if err != nil {
		return &exitError{err: fmt.Errorf("load configuration: %w", err), msg: failure, code: 1}
	}

	// Write the file to the working directory, where the instructions below tell the user to copy it from.
	fileName, err := upgrade.WriteEnvFile(log, ".", cfg.Notify)
	if err != nil {
		return &exitError{err: err, msg: failure, code: 1}
	}

	// Attempt to retrieve the running container's ID to provide precise instructions for copying the file from the container.
	// Use a placeholder ("<CONTAINER>") if this fails, ensuring the user still gets actionable guidance.
	containerID := "<CONTAINER>"

	cid, err := containerClient.GetContainerIDFromMountinfo(log, p.fsys)
	if err == nil && cid != "" {
		containerID = cid.ShortID()
	} else {
		cid, err = containerClient.GetContainerIDFromCgroupFile(log, p.fsys)
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

	// Wait for the timeout or a stop signal, which cancels the command's
	// context, then remove the file.
	timer := time.NewTimer(cleanupTimeout)
	defer timer.Stop()

	ctx := cmd.Context()

	select {
	case <-timer.C:
		log.Info().Msg("Timed out")
	case <-ctx.Done():
		log.Info().Msg("Stopping")
		// The cause names the signal that stopped the process.
		log.Debug().Err(context.Cause(ctx)).Msg("Stop requested")
	}

	upgrade.RemoveEnvFile(log, fileName)

	return nil
}
