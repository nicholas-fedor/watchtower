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

// errListContainersFailed indicates a failure to list containers.
// It is used to wrap errors from client.ListContainers for consistent error handling.
var errListContainersFailed = errors.New("failed to list containers")

// errSortDependenciesFailed indicates a failure to sort containers by dependencies.
// It is used to wrap errors from sorter.SortByDependencies.
var errSortDependenciesFailed = errors.New("failed to sort containers by dependencies")

// errVerifyConfigFailed indicates a failure to verify container configuration.
// It is used to wrap errors from VerifyConfiguration for consistency.
var errVerifyConfigFailed = errors.New("failed to verify container configuration")

// errPreUpdateFailed indicates a failure in the pre-update lifecycle command.
// It is used to wrap errors from ExecutePreUpdateCommand.
var errPreUpdateFailed = errors.New("pre-update command failed")

// errSkipUpdate signals that a container update should be skipped due to a specific condition.
// It is used when the pre-update command returns exit code 75 (EX_TEMPFAIL).
var errSkipUpdate = errors.New(
	"skipping container due to pre-update command exit code 75 (EX_TEMPFAIL)",
)

// errStopContainerFailed indicates a failure to stop a container.
// It is used to wrap errors from client.StopContainer for consistent error handling.
var errStopContainerFailed = errors.New("failed to stop container")

// errStartContainerFailed indicates a failure to start a container.
// It is used to wrap errors from client.StartContainer for consistent error handling.
var errStartContainerFailed = errors.New("failed to start container")

// Update examines running Docker containers for outdated images and updates them.
// It scans containers, sorts them by dependencies, and performs updates based on provided parameters,
// returning a report of the operation’s outcome.
func Update(client container.Client, params types.UpdateParams) (types.Report, error) {
	logrus.Debug("Checking containers for updated images")

	progress := &session.Progress{}
	staleCount := 0

	if params.LifecycleHooks {
		lifecycle.ExecutePreChecks(client, params)
	}

	containers, err := client.ListContainers(params.Filter)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errListContainersFailed, err)
	}

	staleCheckFailed := 0

	for i, targetContainer := range containers {
		stale, newestImage, err := client.IsContainerStale(targetContainer, params)
		shouldUpdate := stale && !params.NoRestart && !targetContainer.IsMonitorOnly(params)

		if err == nil && shouldUpdate {
			// Verify configuration for recreating the container.
			err = targetContainer.VerifyConfiguration()
			if err != nil && logrus.IsLevelEnabled(logrus.TraceLevel) {
				imageInfo := targetContainer.ImageInfo()
				logrus.Tracef("Image info: %#v", imageInfo)
				logrus.Tracef("Container info: %#v", targetContainer.ContainerInfo())

				if imageInfo != nil {
					logrus.Tracef("Image config: %#v", imageInfo.Config)
				}
			}
		}

		if err != nil {
			logrus.Infof(
				"Unable to update container %q: %v. Proceeding to next.",
				targetContainer.Name(),
				err,
			)

			stale = false
			staleCheckFailed++

			progress.AddSkipped(targetContainer, err)
		} else {
			progress.AddScanned(targetContainer, newestImage)
		}

		containers[i].SetStale(stale)

		if stale {
			staleCount++
		}
	}

	containers, err = sorter.SortByDependencies(containers)
	if err != nil {
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

	if params.RollingRestart {
		progress.UpdateFailed(performRollingRestart(containersToUpdate, client, params))
	} else {
		failedStop, stoppedImages := stopContainersInReversedOrder(containersToUpdate, client, params)
		progress.UpdateFailed(failedStop)

		failedStart := restartContainersInSortedOrder(containersToUpdate, client, params, stoppedImages)
		progress.UpdateFailed(failedStart)
	}

	if params.LifecycleHooks {
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
		if containers[i].ToRestart() {
			err := stopStaleContainer(containers[i], client, params)
			if err != nil {
				failed[containers[i].ID()] = err
			} else {
				if err := restartStaleContainer(containers[i], client, params); err != nil {
					failed[containers[i].ID()] = err
				} else if containers[i].IsStale() {
					// Only add (previously) stale containers' images to cleanup
					cleanupImageIDs[containers[i].ImageID()] = true
				}
			}
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
		if err := stopStaleContainer(containers[i], client, params); err != nil {
			failed[containers[i].ID()] = err
		} else {
			// Note: If a container is restarted due to a dependency, this might be empty.
			stopped[containers[i].SafeImageID()] = true
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
	if container.IsWatchtower() {
		logrus.Debugf("This is the watchtower container %s", container.Name())

		return nil
	}

	if !container.ToRestart() {
		return nil
	}

	// Prevent stopping a linked container we cannot restart by verifying its configuration.
	if container.IsLinkedToRestarting() {
		if err := container.VerifyConfiguration(); err != nil {
			return fmt.Errorf("%w: %w", errVerifyConfigFailed, err)
		}
	}

	if params.LifecycleHooks {
		skipUpdate, err := lifecycle.ExecutePreUpdateCommand(client, container)
		if err != nil {
			logrus.Error(err)
			logrus.Info("Skipping container as the pre-update command failed")

			return fmt.Errorf("%w: %w", errPreUpdateFailed, err)
		}

		if skipUpdate {
			logrus.Debug(
				"Skipping container as the pre-update command returned exit code 75 (EX_TEMPFAIL)",
			)

			return errSkipUpdate
		}
	}

	if err := client.StopContainer(container, params.Timeout); err != nil {
		logrus.Error(err)

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

		if stoppedImages[c.SafeImageID()] {
			if err := restartStaleContainer(c, client, params); err != nil {
				failed[c.ID()] = err
			} else if c.IsStale() {
				// Only add (previously) stale containers' images to cleanup
				cleanupImageIDs[c.ImageID()] = true
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
			logrus.Error(err)
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
	// Rename the current Watchtower instance to free its name for the new one.
	if container.IsWatchtower() {
		if err := client.RenameContainer(container, util.RandName()); err != nil {
			logrus.Error(err)

			return nil // Continue despite error per original logic.
		}
	}

	if !params.NoRestart {
		if newContainerID, err := client.StartContainer(container); err != nil {
			logrus.Error(err)

			return fmt.Errorf("%w: %w", errStartContainerFailed, err)
		} else if container.ToRestart() && params.LifecycleHooks {
			lifecycle.ExecutePostUpdateCommand(client, newContainerID)
		}
	}

	return nil
}

// UpdateImplicitRestart updates containers’ LinkedToRestarting flag based on linked dependencies.
// It marks containers linked to restarting ones, ensuring dependent updates are triggered.
func UpdateImplicitRestart(containers []types.Container) {
	for containerIndex, c := range containers {
		if c.ToRestart() {
			// Skip if already marked for restart.
			continue
		}

		if link := linkedContainerMarkedForRestart(c.Links(), containers); link != "" {
			logrus.WithFields(logrus.Fields{
				"restarting": link,
				"linked":     c.Name(),
			}).Debug("container is linked to restarting")
			// Mutate the original array entry, not the loop variable copy.
			containers[containerIndex].SetLinkedToRestarting(true)
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
