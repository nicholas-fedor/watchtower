// Package scheduling provides functionality for scheduling and executing container updates in Watchtower.
// It handles periodic scheduling using cron specifications, manages update concurrency, and ensures
// graceful shutdown of scheduled operations.
package scheduling

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// WaitForRunningUpdate waits for any currently running update to complete before proceeding with shutdown.
// It checks the lock channel status and blocks with a timeout if an update is in progress.
// Parameters:
//   - ctx: The context for cancellation, allowing early shutdown on context timeout.
//   - lock: The channel used to synchronize updates, ensuring only one runs at a time.
func WaitForRunningUpdate(ctx context.Context, lock chan bool) {
	const updateWaitTimeout = 30 * time.Second

	logrus.Debug("Checking lock status before shutdown.")

	if len(lock) == 0 {
		select {
		case <-lock:
			logrus.Debug("Lock acquired, update finished.")
		case <-time.After(updateWaitTimeout):
			logrus.Warn("Timeout waiting for running update to finish, proceeding with shutdown.")
		case <-ctx.Done():
			logrus.Debug("Context cancelled, proceeding with shutdown.")
		}
	} else {
		logrus.Debug("No update running, lock available.")
	}

	logrus.Debug("Lock check completed.")
}

// RunUpgradesOnSchedule schedules and executes periodic container updates according to the cron specification.
//
// It sets up a cron scheduler, runs updates at specified intervals, and ensures graceful shutdown on interrupt
// signals (SIGINT, SIGTERM) or context cancellation, handling concurrency with a lock channel.
// If update-on-start is enabled, it triggers the first update immediately before starting the scheduler.
//
// Parameters:
//   - ctx: The context controlling the scheduler's lifecycle, enabling shutdown on cancellation.
//   - c: The cobra.Command instance, providing access to flags for startup messaging.
//   - filter: The types.Filter determining which containers are updated.
//   - filtering: A string describing the filter, used in startup messaging.
//   - lock: A channel ensuring only one update runs at a time, or nil to create a new one.
//   - cleanup: Boolean indicating whether to remove old images after updates.
//   - scheduleSpec: The cron-formatted schedule string that dictates when periodic container updates occur.
//   - writeStartupMessage: Function to write the startup message with scheduling information.
//   - runUpdatesWithNotifications: Function to perform container updates and send notifications.
//   - client: The Docker client instance used for container operations.
//   - scope: Defines a specific operational scope for Watchtower, limiting updates to containers matching this scope.
//   - notifier: The notification system instance responsible for sending update status messages.
//   - metaVersion: The version string for Watchtower, used in startup messaging.
//   - updateOnStart: Boolean indicating whether to perform an update immediately on startup.
//
// Returns:
//   - error: An error if scheduling fails (e.g., invalid cron spec), nil on successful shutdown.
func RunUpgradesOnSchedule(
	ctx context.Context,
	c *cobra.Command,
	filter types.Filter,
	filtering string,
	lock chan bool,
	cleanup bool,
	scheduleSpec string,
	writeStartupMessage func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string),
	runUpdatesWithNotifications func(types.Filter, bool) *metrics.Metric,
	client container.Client,
	scope string,
	notifier types.Notifier,
	metaVersion string,
	updateOnStart bool,
) error {
	// Initialize lock if not provided, ensuring single-update concurrency.
	if lock == nil {
		lock = make(chan bool, 1)
		lock <- true
	}

	// Create a new cron scheduler for managing periodic updates.
	scheduler := cron.New()

	// Define the update function to be used both for scheduled runs and immediate execution.
	updateFunc := func() {
		select {
		case v := <-lock:
			defer func() { lock <- v }()

			metric := runUpdatesWithNotifications(filter, cleanup)
			metrics.Default().RegisterScan(metric)
		default:
			metrics.Default().RegisterScan(nil)
			logrus.Debug("Skipped another update already running.")
		}

		nextRuns := scheduler.Entries()
		if len(nextRuns) > 0 {
			logrus.Debug("Scheduled next run: " + nextRuns[0].Next.String())
		}
	}

	// Add the update function to the cron schedule, handling concurrency and metrics.
	if scheduleSpec != "" {
		if err := scheduler.AddFunc(
			scheduleSpec,
			updateFunc); err != nil {
			return fmt.Errorf("failed to schedule updates: %w", err)
		}
	}

	// Log startup message with the first scheduled run time.
	var nextRun time.Time
	if len(scheduler.Entries()) > 0 {
		nextRun = scheduler.Entries()[0].Schedule.Next(time.Now())
	}

	writeStartupMessage(c, nextRun, filtering, scope, client, notifier, metaVersion)

	// Check if update-on-start is enabled and trigger immediate update if so.
	if updateOnStart {
		updateFunc()
	}

	// Start the scheduler to begin periodic execution.
	scheduler.Start()

	// Set up signal handling for graceful shutdown.
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal or context cancellation.
	select {
	case <-ctx.Done():
		logrus.Debug("Context canceled, stopping scheduler...")
	case <-interrupt:
		logrus.Debug("Received interrupt signal, stopping scheduler...")
	}

	// Stop the scheduler and wait for any running update to complete.
	scheduler.Stop()
	logrus.Debug("Waiting for running update to be finished...")

	WaitForRunningUpdate(ctx, lock)

	logrus.Debug("Scheduler stopped and update completed.")

	return nil
}
