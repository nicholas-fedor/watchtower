// Package actions provides core logic for Watchtower’s container update operations.
package actions

import (
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/lifecycle"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for update operations.
var (
	// errSortDependenciesFailed indicates a failure to sort containers by dependencies.
	errSortDependenciesFailed = errors.New("failed to sort containers by dependencies")
	// errVerifyConfigFailed indicates a failure to verify container configuration for recreation.
	errVerifyConfigFailed = errors.New("failed to verify container configuration")
	// errPreUpdateFailed indicates a failure in the pre-update lifecycle command execution.
	errPreUpdateFailed = errors.New("pre-update command failed")
	// errSkipUpdate signals that a container update should be skipped due to a specific condition (e.g., EX_TEMPFAIL).
	errSkipUpdate = errors.New(
		"skipping container due to pre-update command exit code 75 (EX_TEMPFAIL)",
	)
	// errRenameWatchtowerFailed indicates a failure to rename the Watchtower container before restarting.
	errRenameWatchtowerFailed = errors.New("failed to rename Watchtower container")
	// errStopContainerFailed indicates a failure to stop a container during the update process.
	errStopContainerFailed = errors.New("failed to stop container")
	// errStartContainerFailed indicates a failure to start a container after an update.
	errStartContainerFailed = errors.New("failed to start container")
)

// Update examines running Docker containers for outdated images and updates them.
// It scans containers, sorts them by dependencies, and performs updates based on provided parameters,
// returning a report of the operation’s outcome.
func Update(client container.Client, params types.UpdateParams) (types.Report, error) {
	logrus.Debug("Starting container update check")

	progress := &session.Progress{}
	staleCount := 0

	if params.LifecycleHooks {
		logrus.Debug("Executing pre-check lifecycle hooks")
		lifecycle.ExecutePreChecks(client, params)
	}

	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		logrus.WithError(err).Debug("Failed to list containers")

		return nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	logrus.WithField("count", len(containers)).Debug("Retrieved containers for update check")

	staleCheckFailed := 0

	for i, sourceContainer := range containers {
		stale, newestImage, err := client.IsContainerStale(sourceContainer, params)
		shouldUpdate := stale && !params.NoRestart && !sourceContainer.IsMonitorOnly(params)

		fields := logrus.Fields{
			"container": sourceContainer.Name(),
			"image":     sourceContainer.ImageName(),
		}

		if err == nil && shouldUpdate {
			// Verify configuration for recreating the container.
			err = sourceContainer.VerifyConfiguration()
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"container_name": sourceContainer.Name(),
					"container_id":   sourceContainer.ID().ShortID(),
					"image_name":     sourceContainer.ImageName(),
					"image_id":       sourceContainer.ID().ShortID(),
				}).Debug("Failed to verify container configuration")
			}
		}

		if err != nil {
			logrus.WithFields(fields).
				WithError(err).
				Debug("Cannot update container, skipping")

			stale = false
			staleCheckFailed++

			progress.AddSkipped(sourceContainer, err)
		} else {
			logrus.WithFields(fields).WithFields(logrus.Fields{
				"stale":        stale,
				"newest_image": newestImage,
			}).Debug("Checked container staleness")
			progress.AddScanned(sourceContainer, newestImage)
		}

		containers[i].SetStale(stale)

		if stale {
			staleCount++
		}
	}

	logrus.WithFields(logrus.Fields{
		"total":  len(containers),
		"stale":  staleCount,
		"failed": staleCheckFailed,
	}).Info("Completed container staleness check")

	containers, err = sorter.SortByDependencies(containers)
	if err != nil {
		logrus.WithError(err).Debug("Failed to sort containers by dependencies")

		return nil, fmt.Errorf("%w: %w", errSortDependenciesFailed, err)
	}

	UpdateImplicitRestart(containers)

	var containersToUpdate []types.Container

	for _, c := range containers {
		if !c.IsMonitorOnly(params) {
			containersToUpdate = append(containersToUpdate, c)
			progress.MarkForUpdate(c.ID())
		}
	}

	logrus.WithField("count", len(containersToUpdate)).Debug("Prepared containers for update")

	if params.RollingRestart {
		progress.UpdateFailed(performRollingRestart(containersToUpdate, client, params))
	} else {
		failedStop, stoppedImages := stopContainersInReversedOrder(containersToUpdate, client, params)
		progress.UpdateFailed(failedStop)

		failedStart := restartContainersInSortedOrder(containersToUpdate, client, params, stoppedImages)
		progress.UpdateFailed(failedStart)
	}

	if params.LifecycleHooks {
		logrus.Debug("Executing post-check lifecycle hooks")
		lifecycle.ExecutePostChecks(client, params)
	}

	return progress.Report(), nil
}

// performRollingRestart updates containers using a rolling restart strategy.
// It processes containers in reverse order, stopping and restarting stale ones,
// and returns a map of container IDs to any errors encountered.
func performRollingRestart(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
) map[types.ContainerID]error {
	cleanupImageIDs := make(map[types.ImageID]bool, len(containers))
	failed := make(map[types.ContainerID]error, len(containers))

	for i := len(containers) - 1; i >= 0; i-- {
		// Only add (previously) stale containers' images to cleanup
		c := containers[i]
		if !c.ToRestart() {
			continue
		}

		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		logrus.WithFields(fields).Debug("Processing container for rolling restart")

		if err := stopStaleContainer(c, client, params); err != nil {
			failed[c.ID()] = err
		} else if err := restartStaleContainer(c, client, params); err != nil {
			failed[c.ID()] = err
		} else if c.IsStale() {
			cleanupImageIDs[c.ImageID()] = true

			logrus.WithFields(fields).Info("Updated container")
		}
	}

	if params.Cleanup {
		cleanupImages(client, cleanupImageIDs)
	}

	return failed
}

// stopContainersInReversedOrder stops containers in reverse order based on their update needs.
// It returns maps of failed stops and stopped image IDs for further processing.
func stopContainersInReversedOrder(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
) (map[types.ContainerID]error, map[types.ImageID]bool) {
	failed := make(map[types.ContainerID]error, len(containers))
	stopped := make(map[types.ImageID]bool, len(containers))

	for i := len(containers) - 1; i >= 0; i-- {
		c := containers[i]
		if err := stopStaleContainer(c, client, params); err != nil {
			failed[c.ID()] = err
		} else {
			stopped[c.SafeImageID()] = true

			logrus.WithFields(logrus.Fields{
				"container": c.Name(),
				"image":     c.ImageName(),
			}).Debug("Stopped container")
		}
	}

	return failed, stopped
}

// stopStaleContainer stops a container if it is stale and eligible for update.
// It skips Watchtower containers and non-restart candidates, handling lifecycle hooks if enabled.
func stopStaleContainer(
	container types.Container,
	client container.Client,
	params types.UpdateParams,
) error {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	if container.IsWatchtower() {
		logrus.WithFields(fields).Debug("Skipping Watchtower container")

		return nil
	}

	if !container.ToRestart() {
		return nil
	}

	// Prevent stopping a linked container we cannot restart by verifying its configuration.
	if container.IsLinkedToRestarting() {
		if err := container.VerifyConfiguration(); err != nil {
			logrus.WithFields(fields).
				WithError(err).
				Debug("Failed to verify container configuration")

			return fmt.Errorf("%w: %w", errVerifyConfigFailed, err)
		}
	}

	if params.LifecycleHooks {
		skipUpdate, err := lifecycle.ExecutePreUpdateCommand(client, container)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Pre-update command execution failed")

			return fmt.Errorf("%w: %w", errPreUpdateFailed, err)
		}

		if skipUpdate {
			logrus.WithFields(fields).Debug("Skipping container due to pre-update exit code 75")

			return errSkipUpdate
		}
	}

	if err := client.StopContainer(container, params.Timeout); err != nil {
		logrus.WithFields(fields).WithError(err).Error("Failed to stop container")

		return fmt.Errorf("%w: %w", errStopContainerFailed, err)
	}

	return nil
}

// restartContainersInSortedOrder restarts containers in sorted order based on prior stops.
// It processes only previously stopped containers and returns a map of failed restarts.
func restartContainersInSortedOrder(
	containers []types.Container,
	client container.Client,
	params types.UpdateParams,
	stoppedImages map[types.ImageID]bool,
) map[types.ContainerID]error {
	cleanupImageIDs := make(map[types.ImageID]bool, len(containers))
	failed := make(map[types.ContainerID]error, len(containers))

	for _, c := range containers {
		if !c.ToRestart() {
			continue
		}

		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		if stoppedImages[c.SafeImageID()] {
			if err := restartStaleContainer(c, client, params); err != nil {
				failed[c.ID()] = err
			} else {
				logrus.WithFields(fields).Debug("Restarted container")

				if c.IsStale() {
					cleanupImageIDs[c.ImageID()] = true
				}
			}
		}
	}

	if params.Cleanup {
		cleanupImages(client, cleanupImageIDs)
	}

	return failed
}

// cleanupImages removes specified image IDs from the client.
// It skips empty IDs and logs any errors encountered during removal.
func cleanupImages(client container.Client, imageIDs map[types.ImageID]bool) {
	for imageID := range imageIDs {
		if imageID == "" {
			continue
		}

		if err := client.RemoveImageByID(imageID); err != nil {
			logrus.WithError(err).WithField("image_id", imageID).Warn("Failed to remove image")
		} else {
			logrus.WithField("image_id", imageID).Debug("Removed image")
		}
	}
}

// restartStaleContainer restarts a stale container, handling Watchtower renaming if needed.
// It starts the new container and executes post-update hooks if applicable, returning any errors.
func restartStaleContainer(
	container types.Container,
	client container.Client,
	params types.UpdateParams,
) error {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Rename the current Watchtower instance to free its name for the new one.
	if container.IsWatchtower() {
		newName := util.RandName()
		if err := client.RenameContainer(container, newName); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"container": container.Name(),
				"new_name":  newName,
			}).Debug("Failed to rename Watchtower container")

			return fmt.Errorf("%w: %w", errRenameWatchtowerFailed, err)
		}

		logrus.WithFields(fields).
			WithField("new_name", newName).
			Debug("Renamed Watchtower container")
	}

	if !params.NoRestart {
		newContainerID, err := client.StartContainer(container)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Failed to start container")

			return fmt.Errorf("%w: %w", errStartContainerFailed, err)
		}

		if container.ToRestart() && params.LifecycleHooks {
			logrus.WithFields(fields).Debug("Executing post-update command")
			lifecycle.ExecutePostUpdateCommand(client, newContainerID)
		}
	}

	return nil
}

// UpdateImplicitRestart updates containers’ LinkedToRestarting flag based on linked dependencies.
// It marks containers linked to restarting ones, ensuring dependent updates are triggered.
func UpdateImplicitRestart(containers []types.Container) {
	for i, c := range containers {
		if c.ToRestart() {
			// Skip if already marked for restart.
			continue
		}

		if link := linkedContainerMarkedForRestart(c.Links(), containers); link != "" {
			logrus.WithFields(logrus.Fields{
				"container":  c.Name(),
				"restarting": link,
			}).Debug("Marked container as linked to restarting")
			// Mutate the original array entry, not the loop variable copy.
			containers[i].SetLinkedToRestarting(true)
		}
	}
}

// linkedContainerMarkedForRestart finds the first linked container marked for restart.
// It returns the name of the linked container if found, or an empty string otherwise.
func linkedContainerMarkedForRestart(links []string, containers []types.Container) string {
	for _, linkName := range links {
		for _, candidate := range containers {
			if candidate.Name() == linkName && candidate.ToRestart() {
				return linkName
			}
		}
	}

	return ""
}
