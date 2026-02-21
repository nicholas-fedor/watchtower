package actions

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// stopContainerTimeout sets the container stop timeout.
const stopContainerTimeout = 10 * time.Minute

// removalRetryDelay sets the delay before retrying removal operations.
const removalRetryDelay = 500 * time.Millisecond

// maxRemovalAttempts sets the maximum number of retries for container removal operations.
const maxRemovalAttempts = 3

// RemoveExcessWatchtowerInstances ensures a single Watchtower instance within the same scope.
//
// It identifies multiple Watchtower containers within the same scope, stops all but the current,
// and collects removed images for deferred removal if enabled, preventing conflicts from concurrent instances.
// Chain identification uses the current container's labels to determine old containers to remove.
// Scoped instances only remove other instances in the same scope, allowing coexistence with different scopes.
// Removal operations respect scope boundaries to prevent cross-scope interference.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - cleanupImages: Remove images if true.
//   - watchtowerScope: Scope to filter Watchtower instances.
//   - removeImageInfos: Pointer to slice of images to remove after stopping excess instances.
//   - currentContainer: The current running Watchtower container.
//
// Returns:
//   - int: Number of removed Watchtower instances.
//   - error: Non-nil if removal fails, nil if single instance or successful removal.
func RemoveExcessWatchtowerInstances(
	ctx context.Context,
	client container.Client,
	cleanupImages bool,
	scope string,
	removeImageInfos *[]types.RemovedImageInfo,
	currentContainer types.Container,
) (int, error) {
	logrus.WithFields(logrus.Fields{
		"scope":          scope,
		"cleanup_images": cleanupImages,
		"current_container_id": func() string {
			if currentContainer != nil {
				return string(currentContainer.ID())
			}

			return ""
		}(),
	}).Debug("Starting removal of excess Watchtower instances")

	// List all containers to find excess instances
	allContainers, err := client.ListContainers(ctx, filters.NoFilter)
	if err != nil {
		return 0, fmt.Errorf("failed to list containers: %w", err)
	}

	// Retrieve containers that are excess Watchtower instances within the same scope
	excessWatchtowerContainers := getExcessContainers(
		scope,
		currentContainer,
		allContainers,
	)

	// If no excess containers found, nothing to remove
	if len(excessWatchtowerContainers) == 0 {
		logrus.WithField("scope", scope).Debug("No excess containers found")

		return 0, nil
	}

	// Stop and remove the excess containers, collecting removed image info if removal is enabled
	removed, err := removeExcessContainers(
		ctx,
		client,
		excessWatchtowerContainers,
		cleanupImages,
		currentContainer,
		removeImageInfos,
	)
	if err != nil {
		return 0, err
	}

	return removed, nil
}

// getExcessContainers retrieves a list of excess Watchtower containers that should be removed.
//
// It identifies containers that are duplicates within the same scope or part of a container chain,
// excluding the current running container, to ensure only one Watchtower instance operates per scope.
//
// Parameters:
//   - watchtowerScope: Scope to filter containers, empty for unscoped.
//   - currentContainer: The current running Watchtower container (nil if not applicable).
//   - allContainers: All containers to search for excess instances.
//
// Returns:
//   - []types.Container: Slice of containers to remove.
func getExcessContainers(
	watchtowerScope string,
	currentContainer types.Container,
	allContainers []types.Container,
) []types.Container {
	logrus.WithFields(logrus.Fields{
		"scope": watchtowerScope,
		"current_container_id": func() string {
			if currentContainer != nil {
				return string(currentContainer.ID())
			}

			return ""
		}(),
	}).Debug("Retrieving excess containers")

	filteredContainers := getFilteredContainers(
		watchtowerScope,
		currentContainer,
		allContainers,
	)

	var chainedContainers []types.Container
	if currentContainer != nil {
		chainedContainers = getChainedContainers(allContainers, currentContainer)
	}

	excessContainers := addExcessContainers(filteredContainers, chainedContainers)

	return excessContainers
}

// getFilteredContainers retrieves filtered containers excluding the current one if more than one exist.
//
// Parameters:
//   - scope: Scope UID to filter containers, empty for unscoped.
//   - currentContainer: The current running Watchtower container (nil if not applicable).
//   - allContainers: All containers to filter from.
//
// Returns:
//   - []types.Container: Slice of excess containers to remove.
func getFilteredContainers(
	scope string,
	currentContainer types.Container,
	allContainers []types.Container,
) []types.Container {
	if currentContainer == nil {
		return []types.Container{}
	}

	var filter types.Filter

	switch {
	case scope != "":
		filter = filters.FilterByScope(scope, filters.WatchtowerContainersFilter)
	case scope == "":
		filter = filters.UnscopedWatchtowerContainersFilter
	}

	var filteredContainers []types.Container

	for _, c := range allContainers {
		if filter == nil || filter(c) {
			filteredContainers = append(filteredContainers, c)
		}
	}

	var excessContainers []types.Container

	for _, c := range filteredContainers {
		if string(c.ID()) != string(currentContainer.ID()) {
			excessContainers = append(excessContainers, c)
		}
	}

	logrus.WithFields(logrus.Fields{
		"scope":                     scope,
		"excess_containers_found":   len(excessContainers),
		"filtered_containers_total": len(filteredContainers),
	}).Debug("Filtered excess containers")

	return excessContainers
}

// getChainedContainers retrieves containers linked in a chain based on the current container's chain label.
//
// It parses the container chain label from the current container, identifies all linked containers
// excluding the current one, and returns them as a slice. If the current container has
// no chain label or an empty chain label, an empty slice is returned.
//
// Parameters:
//   - allContainers: All containers to search for chained containers.
//   - currentContainer: The current running Watchtower container (nil if not applicable).
//
// Returns:
//   - []types.Container: Slice of chained containers excluding the current one.
func getChainedContainers(
	allContainers []types.Container,
	currentContainer types.Container,
) []types.Container {
	var chainedContainers []types.Container

	// Get the current Watchtower container's com.centurylinklabs.watchtower.container-chain label.
	chainLabelValue, present := currentContainer.GetContainerChain()

	// If it's not present, there are no chained containers.
	if !present {
		return []types.Container{}
	}

	// If it's empty, there are no chained containers.
	if chainLabelValue == "" {
		return []types.Container{}
	}

	// Split the container chain label value into a slice of container IDs.
	containerChain := strings.Split(chainLabelValue, ",")

	// Create a map of container IDs from the chain for efficient lookup.
	containerChainMap := make(map[string]struct{})
	for _, id := range containerChain {
		containerChainMap[id] = struct{}{}
	}

	// Filter containers that are in the chain, present on the host, and not the current container.
	// Chained containers are parent containers that must be removed regardless of scope.
	for _, c := range allContainers {
		if _, exists := containerChainMap[string(c.ID())]; exists &&
			c.ID() != currentContainer.ID() {
			chainedContainers = append(chainedContainers, c)
		}
	}

	// Return an empty slice if no chained containers are found
	if len(chainedContainers) == 0 {
		return []types.Container{}
	}

	return chainedContainers
}

// addExcessContainers combines and deduplicates excess and chain containers for removal.
//
// It creates a map to deduplicate containers by ID, adding both excess and chain containers,
// then returns a slice of unique containers to remove.
//
// Parameters:
//   - excessContainers: Containers identified as excess within the scope.
//   - chainContainers: Containers linked in a chain excluding the current one.
//
// Returns:
//   - []types.Container: Deduplicated slice of containers to remove.
func addExcessContainers(excessContainers, chainContainers []types.Container) []types.Container {
	containersToRemoveMap := make(map[types.ContainerID]types.Container)
	for _, c := range excessContainers {
		containersToRemoveMap[c.ID()] = c
	}

	for _, c := range chainContainers {
		containersToRemoveMap[c.ID()] = c
	}

	containersToRemove := make([]types.Container, 0, len(containersToRemoveMap))
	for _, c := range containersToRemoveMap {
		containersToRemove = append(containersToRemove, c)
	}

	return containersToRemove
}

// removeExcessContainers attempts to stop and remove a list of excess containers with retries.
//
// It stops and removes the provided containers, handling retries on failure, tracks removal successes,
// and optionally collects image information for deferred removal if images should be cleaned up.
// Excludes the current running container from removal and manages image cleanup based on removal success.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - excessWatchtowerContainers: Slice of Watchtower containers to stop and remove.
//   - cleanupImages: Remove images if true.
//   - currentContainer: The current running Watchtower container (nil if not applicable).
//   - removeImageInfos: Pointer to slice of images to remove after stopping excess instances.
//
// Returns:
//   - int: Number of successfully removed containers.
//   - error: Non-nil if any container removal failed or insufficient removals occurred.
func removeExcessContainers(
	ctx context.Context,
	client container.Client,
	excessWatchtowerContainers []types.Container,
	cleanupImages bool,
	currentContainer types.Container,
	removeImageInfos *[]types.RemovedImageInfo,
) (int, error) {
	logrus.WithFields(logrus.Fields{
		"excess_count":   len(excessWatchtowerContainers),
		"cleanup_images": cleanupImages,
	}).Debug("Starting removal of excess containers")

	localRemoved := []types.RemovedImageInfo{}

	var collectedInfos *[]types.RemovedImageInfo
	if removeImageInfos != nil {
		collectedInfos = removeImageInfos
	} else {
		collectedInfos = &localRemoved
	}

	excessInstancesRemoved := 0

	for _, c := range excessWatchtowerContainers {
		logrus.WithFields(logrus.Fields{
			"container_id":   string(c.ID()),
			"container_name": c.Name(),
		}).Debug("Starting removal attempts for excess container")

		succeeded := false
		wasNotFound := false

		for attempt := range maxRemovalAttempts {
			logrus.WithFields(logrus.Fields{
				"container_id": string(c.ID()),
				"attempt":      attempt + 1,
				"max_attempts": maxRemovalAttempts,
			}).Debug("Attempting to stop and remove container")

			err := client.StopAndRemoveContainer(ctx, c, stopContainerTimeout)
			if err == nil {
				logrus.WithFields(logrus.Fields{
					"container_id": string(c.ID()),
					"attempt":      attempt + 1,
				}).Debug("Successfully stopped and removed container")

				succeeded = true

				break
			}

			if cerrdefs.IsNotFound(err) {
				logrus.WithFields(logrus.Fields{
					"container_id": string(c.ID()),
					"attempt":      attempt + 1,
				}).Debug("Container not found, considering as removed")

				succeeded = true
				wasNotFound = true

				break
			}

			logrus.WithError(err).WithFields(logrus.Fields{
				"container_id": string(c.ID()),
				"attempt":      attempt + 1,
			}).Debug("Failed to stop and remove container")

			if attempt < maxRemovalAttempts-1 {
				select {
				case <-time.After(removalRetryDelay):
					// continue to next retry attempt
				case <-ctx.Done():
					return 0, fmt.Errorf("context cancelled during retry delay: %w", ctx.Err())
				}
			}
		}

		if succeeded {
			excessInstancesRemoved++

			if cleanupImages && currentContainer != nil &&
				c.ImageID() != currentContainer.ImageID() &&
				!wasNotFound {
				logrus.WithFields(logrus.Fields{
					"container_id": string(c.ID()),
					"image_id":     string(c.ImageID()),
					"image_name":   c.ImageName(),
				}).Debug("Collecting image info for deferred removal")

				*collectedInfos = append(*collectedInfos, types.RemovedImageInfo{
					ImageID:       c.ImageID(),
					ContainerID:   c.ID(),
					ImageName:     c.ImageName(),
					ContainerName: c.Name(),
				})
			}
		}
	}

	if excessInstancesRemoved < len(excessWatchtowerContainers) {
		*collectedInfos = nil
	}

	if cleanupImages {
		removedInfos, err := RemoveImages(ctx, client, *collectedInfos)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"removed_images_count": len(removedInfos),
				"image_infos":          removedInfos,
				"cleanup_images":       true,
			}).Error("failed to remove excess images")
		}

		if removeImageInfos != nil {
			*removeImageInfos = removedInfos
		}
	}

	if excessInstancesRemoved < len(excessWatchtowerContainers) {
		return 0, fmt.Errorf(
			"%w: %d of %d instances failed to stop",
			errStopWatchtowerFailed,
			len(excessWatchtowerContainers)-excessInstancesRemoved,
			len(excessWatchtowerContainers),
		)
	}

	logrus.WithField("removed_instances", excessInstancesRemoved).
		Info("Successfully removed all excess Watchtower instances")

	return excessInstancesRemoved, nil
}

// RemoveImages removes specified images and returns successfully removed ones.
//
// It iterates through the provided images, attempting to remove each from the Docker environment,
// logging successes or failures for debugging and monitoring. Tracks successfully removed image info.
// If no images are provided, it returns an empty slice and no error.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker operations.
//   - images: Slice of images to remove.
//
// Returns:
//   - []RemovedImageInfo: Slice of successfully removed image info.
//   - error: Non-nil if any image removal failed, nil otherwise.
func RemoveImages(
	ctx context.Context,
	client container.Client,
	images []types.RemovedImageInfo,
) ([]types.RemovedImageInfo, error) {
	// Return early if no images need removal.
	if len(images) == 0 {
		logrus.Debug("No images provided for removal, skipping")

		return []types.RemovedImageInfo{}, nil
	}

	removed := []types.RemovedImageInfo{}

	var removalErrors []error

	for _, image := range images {
		imageID := image.ImageID
		if imageID == "" {
			continue // Skip empty IDs to avoid invalid operations.
		}

		logrus.WithFields(logrus.Fields{
			"image_id":     string(imageID),
			"image_name":   image.ImageName,
			"container_id": string(image.ContainerID),
		}).Debug("Attempting to remove image")

		err := client.RemoveImageByID(ctx, imageID, image.ImageName)
		if err != nil {
			// Check if this is a "not found" error (expected when multiple instances remove the same image)
			switch {
			case cerrdefs.IsNotFound(err):
				logrus.WithFields(logrus.Fields{
					"image_id":   imageID,
					"image_name": image.ImageName,
				}).Debug("Image already removed")
			case cerrdefs.IsConflict(err):
				logrus.WithFields(logrus.Fields{
					"image_id":   imageID,
					"image_name": image.ImageName,
				}).Debug("Image is in use by running container, skipping removal")
			default:
				logrus.WithError(err).WithFields(logrus.Fields{
					"image_id":   imageID,
					"image_name": image.ImageName,
				}).Debug("Failed to remove image")
				removalErrors = append(
					removalErrors,
					fmt.Errorf("failed to remove image %s: %w", imageID, err),
				)
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"image_id":   imageID.ShortID(),
				"image_name": image.ImageName,
			}).Debug("Removed old image")
			removed = append(
				removed,
				types.RemovedImageInfo{
					ImageID:       imageID,
					ContainerID:   image.ContainerID,
					ImageName:     image.ImageName,
					ContainerName: image.ContainerName,
				},
			)
		}
	}

	if len(removalErrors) > 0 {
		return removed, fmt.Errorf(
			"%w: %d of %d image removals failed",
			errImageRemovalFailed,
			len(removalErrors),
			len(images),
		)
	}

	return removed, nil
}

// containerNames extracts names from a container list.
//
// It creates a slice of container names for logging or debugging purposes, preserving order.
//
// Parameters:
//   - containers: List of containers.
//
// Returns:
//   - []string: List of container names.
func containerNames(containers []types.Container) []string {
	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = c.Name()
	}

	return names
}
