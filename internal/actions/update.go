package actions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/sirupsen/logrus"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/compose"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/lifecycle"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// defaultPullFailureDelay defines the default delay duration for failed Watchtower self-update pulls.
const defaultPullFailureDelay = 5 * time.Minute

// defaultHealthCheckTimeout defines the default timeout for waiting for container health checks.
const defaultHealthCheckTimeout = 5 * time.Minute

// projectServiceParts defines the expected number of parts in a project-service link format.
const projectServiceParts = 2

// Update scans and updates containers based on parameters.
//
// It checks container staleness, sorts by dependencies, and updates or restarts containers as needed,
// collecting cleaned image info for cleanup. Non-stale linked containers are restarted but not marked as updated.
// Containers with pinned images (referenced by digest) are skipped to preserve immutability.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for interacting with Docker API.
//   - config: UpdateParams specifying behavior like cleanup, restart, and filtering.
//
// Returns:
//   - types.Report: Session report summarizing scanned, updated, and failed containers.
//   - []types.RemovedImageInfo: Slice of cleaned image info to clean up after updates.
//   - error: Non-nil if listing or sorting fails, nil on success.
func Update(
	ctx context.Context,
	client container.Client,
	config types.UpdateParams,
) (types.Report, []types.RemovedImageInfo, error) {
	// Check for context cancellation early
	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("update cancelled: %w", ctx.Err())
	default:
	}

	// Initialize logging for the update process start.
	logrus.Debug("Starting container update check")

	// Fetch all containers for monitoring
	allContainers, err := client.ListContainers(ctx, filters.NoFilter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Create a progress tracker for reporting scanned, updated, and skipped containers.
	progress := &session.Progress{}
	// Track the number of stale containers for logging.
	staleCount := 0
	// Initialize a slice to collect cleaned image info for cleanup after updates.
	cleanupImageInfos := []types.RemovedImageInfo{}
	// Track if Watchtower self-update pull failed to add safeguard delay.
	watchtowerPullFailed := false

	// Run pre-check lifecycle hooks if enabled to validate the environment before updates.
	if config.LifecycleHooks {
		logrus.Debug("Executing pre-check lifecycle hooks")
		lifecycle.ExecutePreChecks(ctx, client, config)
	}

	// Filter containers based on the provided filter (e.g., all, specific names).
	var filteredContainers []types.Container

	for _, c := range allContainers {
		if config.Filter == nil || config.Filter(c) {
			filteredContainers = append(filteredContainers, c)
		}
	}

	// Prepare a list of container names and images for detailed debugging output.
	filteredContainerNames := make([]string, len(filteredContainers))
	for i, c := range filteredContainers {
		filteredContainerNames[i] = fmt.Sprintf("%s (%s)", c.Name(), c.ImageName())
	}
	// Log the retrieved containers and filter details.
	logrus.WithFields(logrus.Fields{
		"count":      len(filteredContainers),
		"containers": filteredContainerNames,
		"filter":     fmt.Sprintf("%T", config.Filter),
	}).Debug("Retrieved containers for update check")

	// Skip monitored containers that reference themselves as dependencies
	// via the Watchtower depends-on label.
	for _, monitoredContainer := range filteredContainers {
		if c, ok := monitoredContainer.(*container.Container); ok {
			if hasSelfDependency(c) {
				progress.AddSkipped(monitoredContainer, errSelfDependency, config)
				logrus.Warnf(
					"Skipping container update (self-dependency): %s (%s)",
					monitoredContainer.Name(),
					monitoredContainer.ID().ShortID(),
				)
			}
		}
	}

	// Detect circular dependencies and mark affected containers as skipped.
	cycles := container.DetectCycles(filteredContainers)
	for _, c := range filteredContainers {
		if cycles[container.ResolveContainerIdentifier(c)] {
			progress.AddSkipped(c, errCircularDependency, config)
			logrus.Warnf(
				"Skipping container update (circular dependency): %s (%s)",
				c.Name(),
				c.ID().ShortID(),
			)
		}
	}

	// Track containers that fail staleness checks for reporting.
	staleCheckFailed := 0

	// Iterate through containers to check staleness and prepare for updates or restarts.
	for i, sourceContainer := range filteredContainers {
		// Check for context cancellation to enable faster shutdown during long update cycles.
		select {
		case <-ctx.Done():
			return progress.Report(), cleanupImageInfos, ctx.Err()
		default:
		}

		// Skip containers already processed (e.g., skipped due to circular dependencies).
		if _, exists := (*progress)[sourceContainer.ID()]; exists {
			continue
		}

		// Set up logging fields for the current container.
		fields := logrus.Fields{
			"container": sourceContainer.Name(),
			"image":     sourceContainer.ImageName(),
		}
		clog := logrus.WithFields(fields)

		// Check if the container uses a pinned (digest-based) image to skip updates.
		isPinned, err := isPinned(sourceContainer, progress, config)
		if err != nil {
			// Log and skip containers with unparsable image references, marking as skipped.
			clog.WithError(err).Debug("Failed to check pinned image, skipping container")
			progress.AddSkipped(
				sourceContainer,
				fmt.Errorf("%w: %w", errParseImageReference, err),
				config,
			)

			staleCheckFailed++

			continue
		}

		if isPinned {
			// Skip staleness checks for pinned images and mark as scanned.
			clog.Debug("Skipping staleness check for pinned image")

			continue
		}

		// Determine if the container is stale and needs updating.
		// If the container is Watchtower and SkipSelfUpdate is enabled, skip the update
		// by setting stale to false and using the current image. Otherwise, check staleness.
		var (
			stale       bool
			newestImage types.ImageID
		)

		if sourceContainer.IsWatchtower() && config.SkipSelfUpdate {
			stale = false
			newestImage = sourceContainer.ImageID()
		} else {
			stale, newestImage, err = client.IsContainerStale(ctx, sourceContainer, config)
		}

		// Determine if the container should be updated based on staleness and config.
		shouldUpdate := shouldUpdateContainer(stale, sourceContainer, config)

		// Log when skipping Watchtower self-update in run-once mode
		if stale && sourceContainer.IsWatchtower() && config.RunOnce {
			clog.Info("Skipping Watchtower self-update in run-once mode")
		}

		// Track old image ID before update for cleanup notifications.
		if shouldUpdate {
			if c, ok := filteredContainers[i].(*container.Container); ok {
				c.SetOldImageID(sourceContainer.ImageID())
			}
		}

		// Verify the container’s configuration if it’s slated for update to ensure recreation is possible.
		if err == nil && shouldUpdate {
			err = sourceContainer.VerifyConfiguration()
			if err != nil {
				// Log configuration verification failure with detailed context.
				logrus.WithError(err).WithFields(logrus.Fields{
					"container_name": sourceContainer.Name(),
					"container_id":   sourceContainer.ID().ShortID(),
					"image_name":     sourceContainer.ImageName(),
					"image_id":       sourceContainer.ImageID().ShortID(),
				}).Debug("Failed to verify container configuration")
			}
		}

		// Handle staleness check results, logging skips or adding to the progress report.
		if err != nil {
			// Skip containers with staleness check errors, marking them as skipped.
			clog.WithError(err).Debug("Cannot update container, skipping")

			stale = false
			staleCheckFailed++

			progress.AddSkipped(sourceContainer, err, config)

			// Track if Watchtower self-update pull failed for safeguard.
			// Only set to true if we actually attempted a self-update (i.e., SkipSelfUpdate is false)
			if sourceContainer.IsWatchtower() && !config.SkipSelfUpdate {
				watchtowerPullFailed = true
			}
		} else {
			// For fresh containers, set newestImage to current image ID for proper categorization
			if !stale {
				newestImage = sourceContainer.ImageID()
			}

			// Log successful staleness check and add to scanned containers.
			clog.WithFields(logrus.Fields{
				"stale":        stale,
				"newest_image": newestImage,
			}).Debug("Checked container staleness")
			progress.AddScanned(sourceContainer, newestImage, config)
		}

		// Update the container’s stale status for dependency sorting.
		// Only mark as stale if the container should actually be updated.
		filteredContainers[i].SetStale(stale && shouldUpdate)

		// Increment stale count for logging summary.
		if stale {
			staleCount++
		}
	}

	// Log the summary of staleness checks, including total, stale, and failed counts.
	logrus.WithFields(logrus.Fields{
		"total":  len(filteredContainers),
		"stale":  staleCount,
		"failed": staleCheckFailed,
	}).Debug("Completed container staleness check")

	// Build a map for a lookup of containers by ID.
	containerByID := make(map[types.ContainerID]types.Container, len(allContainers))
	for _, ac := range allContainers {
		containerByID[ac.ID()] = ac
	}

	// Propagate stale status to allContainers since they are different instances.
	for _, c := range filteredContainers {
		if c.IsStale() {
			if ac, ok := containerByID[c.ID()]; ok {
				ac.SetStale(true)
			}
		}
	}

	// Sort containers by dependencies to ensure correct update and restart order.
	err = sorter.SortByDependencies(filteredContainers)
	if err != nil {
		if errors.Is(err, sorter.ErrCircularReference) {
			var circularErr sorter.CircularReferenceError
			if errors.As(err, &circularErr) {
				circularName := circularErr.ContainerName
				// Find the container and mark as skipped.
				for _, c := range filteredContainers {
					if c.Name() == circularName {
						// Only add if not already skipped (e.g., from initial cycle detection)
						if _, exists := (*progress)[c.ID()]; !exists {
							progress.AddSkipped(c, errCircularDependency, config)
							logrus.Warnf(
								"Skipping container update (circular dependency): %s (%s)",
								c.Name(),
								c.ID().ShortID(),
							)
						}

						break
					}
				}
			}
			// Skip UpdateImplicitRestart to avoid potential issues with circular dependencies.
		} else {
			// Log and return an error if dependency sorting fails for other reasons.
			logrus.WithError(err).Debug("Failed to sort containers by dependencies")

			return nil, []types.RemovedImageInfo{}, fmt.Errorf(
				"%w: %w",
				errSortDependenciesFailed,
				err,
			)
		}
	} else {
		// Mark containers linked to restarting ones for restart without updating.
		UpdateImplicitRestart(allContainers, filteredContainers)
	}

	// Collect all containers to restart (updates and implicit restarts)
	var allContainersToRestart []types.Container

	for _, c := range filteredContainers {
		if c.ToRestart() && !c.IsMonitorOnly(config) {
			allContainersToRestart = append(allContainersToRestart, c)
		}
	}

	// Sort containers to restart by dependencies to ensure correct update and restart order.
	err = sorter.SortByDependencies(allContainersToRestart)
	if err != nil {
		logrus.WithError(err).Debug("Failed to sort all containers to restart by dependencies")

		return nil, []types.RemovedImageInfo{}, fmt.Errorf(
			"%w: %w",
			errSortDependenciesFailed,
			err,
		)
	}

	// Log the number of containers prepared for restart.
	logrus.WithField("restart_count", len(allContainersToRestart)).
		Debug("Prepared containers for restart")

	// Perform updates and restarts, either with rolling restarts or in batches.
	var (
		failedStop    map[types.ContainerID]error
		stoppedImages []types.RemovedImageInfo
		failedStart   map[types.ContainerID]error
	)

	if config.RollingRestart {
		// Apply rolling restarts for all containers in dependency order.
		rollingFailed, rollingErr := performRollingRestart(
			ctx,
			allContainersToRestart,
			client,
			config,
			&cleanupImageInfos,
			progress,
		)
		progress.UpdateFailed(rollingFailed)

		if rollingErr != nil {
			return progress.Report(), cleanupImageInfos, rollingErr
		}
	} else {
		// Mark containers to update for update in progress
		for _, c := range allContainersToRestart {
			if c.IsStale() {
				progress.MarkForUpdate(c.ID())
			}
		}

		// Stop and restart containers in batches, respecting dependency order.
		failedStop, stoppedImages = stopContainersInReversedOrder(
			ctx,
			allContainersToRestart,
			client,
			config,
		)
		progress.UpdateFailed(failedStop)

		failedStart = restartContainersInSortedOrder(
			ctx,
			allContainersToRestart,
			client,
			config,
			stoppedImages,
			&cleanupImageInfos,
			progress,
		)
		progress.UpdateFailed(failedStart)
	}

	// Run post-check lifecycle hooks if enabled to finalize the update process.
	if config.LifecycleHooks {
		logrus.Debug("Executing post-check lifecycle hooks")
		lifecycle.ExecutePostChecks(ctx, client, config)
	}

	// Add safeguard delay if Watchtower self-update pull failed to prevent rapid restarts.
	if watchtowerPullFailed {
		delay := config.PullFailureDelay
		if delay == 0 {
			delay = defaultPullFailureDelay // Default delay
		}

		logrus.WithField("delay", delay).
			Info("Watchtower self-update pull failed, sleeping to prevent rapid restarts")

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			logrus.WithError(ctx.Err()).
				Debug("Context cancelled during pull-failure delay; skipping remaining delay")
		}
	}

	// Return the final report summarizing the session and the cleanup image infos.
	return progress.Report(), cleanupImageInfos, nil
}

// hasSelfDependency checks if a container has a self-dependency in its Watchtower depends-on label.
// It now uses the shared GetLinksFromWatchtowerLabel helper function for parsing and normalization.
func hasSelfDependency(c types.Container) bool {
	sourceContainer, ok := c.(*container.Container)
	if !ok {
		return false
	}

	clog := logrus.WithField("container", c.Name())

	links := container.GetLinksFromWatchtowerLabel(sourceContainer, clog)

	return slices.Contains(links, c.Name())
}

// UpdateImplicitRestart marks containers linked to restarting ones.
//
// It uses a multi-pass algorithm to ensure transitive propagation through the dependency chain,
// continuing until no more containers are marked for restart.
//
// Parameters:
//   - allContainers: List of all containers.
//   - containers: List of containers to update.
func UpdateImplicitRestart(allContainers, containers []types.Container) {
	logrus.Debug("Starting UpdateImplicitRestart")

	byID := make(map[types.ContainerID]types.Container, len(allContainers))

	restartByName := make(map[string]bool, len(allContainers))
	for _, c := range allContainers {
		byID[c.ID()] = c
		restartByName[c.Name()] = c.ToRestart()
	}

	markedContainers := []string{}
	changed := true

	for changed {
		changed = false

		for i, c := range containers {
			if c.ToRestart() {
				continue // Skip already marked containers.
			}

			// c.Links() already returns normalized container names
			links := c.Links()
			containerIdentifier := container.ResolveContainerIdentifier(c)
			logrus.WithFields(logrus.Fields{
				"container":            c.Name(),
				"container_identifier": containerIdentifier,
				"links":                links,
				"to_restart":           c.ToRestart(),
			}).Debug("Checking links for container")

			if link := linkedIdentifierMarkedForRestart(
				links,
				restartByName,
				c,
				allContainers,
			); link != "" {
				logrus.WithFields(logrus.Fields{
					"container":  c.Name(),
					"restarting": link,
				}).Debug("Marked container as linked to restarting")
				containers[i].SetLinkedToRestarting(true)

				if ac, ok := byID[c.ID()]; ok {
					ac.SetLinkedToRestarting(true)
					restartByName[ac.Name()] = true
				}

				markedContainers = append(markedContainers, c.Name())
				changed = true
			}
		}
	}

	logrus.WithField("marked_containers", markedContainers).Debug("Completed UpdateImplicitRestart")
}

// shouldUpdateContainer determines if a container should be updated based on its staleness and update parameters.
//
// It checks multiple conditions:
// - The container must be stale (outdated image)
// - The container must not be monitor-only
// - Updates are allowed unless NoRestart is true and it's not a Watchtower container
// - Watchtower containers are skipped in run-once mode
// - Watchtower self-updates are skipped if SkipSelfUpdate is true
//
// Parameters:
//   - stale: Whether the container's image is outdated.
//   - container: The container to check.
//   - config: Update parameters controlling update behavior.
//
// Returns:
//   - bool: True if the container should be updated, false otherwise.
func shouldUpdateContainer(stale bool, container types.Container, config types.UpdateParams) bool {
	// Must be stale to update
	if !stale {
		return false
	}

	// Skip monitor-only containers
	if container.IsMonitorOnly(config) {
		return false
	}

	// Allow update if NoRestart is false, or if it's a Watchtower container (which can update even with NoRestart)
	if config.NoRestart && !container.IsWatchtower() {
		return false
	}

	// Skip Watchtower self-update in run-once mode
	if config.RunOnce && container.IsWatchtower() {
		return false
	}

	// Skip Watchtower self-update if SkipSelfUpdate is true
	if config.SkipSelfUpdate && container.IsWatchtower() {
		return false
	}

	// Skip other Watchtower containers from self-updates
	if container.IsWatchtower() && config.CurrentContainerID != "" &&
		container.ID() != config.CurrentContainerID {
		return false
	}

	return true
}

// linkedIdentifierMarkedForRestart finds a restarting linked container by identifier.
//
// It searches for a container identifier in the links list that is marked for restart,
// returning its identifier. The matching follows this priority order:
//
//  1. Exact match: Direct identifier match in restartByIdentifier map.
//
//  2. Project-service format (exactly one dash): Matches containers where both the project
//     and service name match the linked container's project and service.
//
//  3. Service-only format (no dash): Prioritizes same-project matches first, then falls back
//     to cross-project matches. This ensures that dependencies within the same Docker Compose
//     project are preferred over external dependencies, while still allowing cross-project
//     dependencies when no same-project match exists.
//
// Parameters:
//   - links: List of linked container identifiers.
//   - restartByIdentifier: Map of container identifiers to restart status.
//   - dependentContainer: The container that has the dependency.
//   - allContainers: List of all containers.
//
// Returns:
//   - string: Identifier of restarting linked container, or empty if none.
func linkedIdentifierMarkedForRestart(
	links []string,
	restartByIdentifier map[string]bool,
	dependentContainer types.Container,
	allContainers []types.Container,
) string {
	nameToContainer := make(map[string]types.Container, len(allContainers))
	for _, c := range allContainers {
		nameToContainer[c.Name()] = c
	}

	dependentProject := getProject(dependentContainer)

	logrus.WithFields(logrus.Fields{
		"links":               links,
		"restartByIdentifier": restartByIdentifier,
		"dependentProject":    dependentProject,
	}).Debug("Searching for restarting linked container")

	for _, link := range links {
		logrus.WithField("checking_link", link).Debug("Checking link for restarting match")

		// Exact match
		if restartByIdentifier[link] {
			logrus.WithField("found_restarting_identifier", link).
				Debug("Found restarting linked container via exact match")

			return link
		}

		if strings.Contains(link, "-") && strings.Count(link, "-") == 1 {
			// project-service format (only if exactly one dash)
			parts := strings.Split(link, "-")
			if len(parts) != projectServiceParts {
				continue
			}

			linkProject := parts[0]
			serviceName := parts[1]

			logrus.WithFields(logrus.Fields{
				"link":        link,
				"linkProject": linkProject,
				"serviceName": serviceName,
			}).Debug("Checking project-service match")

			for identifier, restarting := range restartByIdentifier {
				if restarting && getProject(nameToContainer[identifier]) == linkProject &&
					strings.Contains(identifier, serviceName) {
					logrus.WithFields(logrus.Fields{
						"link":    link,
						"matched": identifier,
						"project": linkProject,
						"service": serviceName,
					}).Debug("Found restarting linked container via project-service match")

					return identifier
				}
			}
		} else {
			// service name only, prioritize same project first, then cross-project dependencies
			logrus.WithFields(logrus.Fields{
				"link":             link,
				"dependentProject": dependentProject,
			}).Debug("Checking service-only match")

			// crossProjectMatch stores the first cross-project match found, used as fallback
			// when no same-project match exists.
			//
			// NOTE: Go map iteration order is non-deterministic, so when multiple cross-project
			// containers match the service name, the first one encountered during iteration is
			// selected. This non-determinism is acceptable for fallback scenarios where no exact
			// or same-project match exists, as it provides a "best effort" approach.
			//
			// The strings.Contains matching is intentional for service-only lookups, allowing
			// service names like "db" to match container identifiers like "project1-db" or
			// "myapp-db". This flexible matching enables shorthand references without requiring
			// the full project-service format.
			var crossProjectMatch string

			// Collect and sort identifiers to ensure deterministic iteration order.
			// This is critical for reproducible behavior: without sorting, the iteration
			// order of restartByIdentifier map would be random, causing different
			// cross-project matches to be selected on each run when multiple candidates
			// exist. Sorting ensures that:
			//   1. The same cross-project container is always selected as fallback
			//   2. Restarts happen in a predictable, testable order
			//   3. Multiple Watchtower runs produce consistent results
			identifiers := make([]string, 0, len(restartByIdentifier))
			for identifier := range restartByIdentifier {
				identifiers = append(identifiers, identifier)
			}
			// Sort alphabetically for deterministic ordering across all identifiers
			slices.Sort(identifiers)

			for _, identifier := range identifiers {
				restarting := restartByIdentifier[identifier]
				if restarting && strings.Contains(identifier, link) {
					matchProject := getProject(nameToContainer[identifier])
					// Priority 1: Same-project match - return immediately
					if matchProject == dependentProject {
						logrus.WithFields(logrus.Fields{
							"link":    link,
							"matched": identifier,
							"project": matchProject,
						}).Debug("Found restarting linked container via same-project service match")

						return identifier
					}
					// Priority 2: Cross-project match - remember first match for fallback.
					// We only store the first match to maintain deterministic behavior.
					// Since identifiers are sorted alphabetically above, the first match
					// in alphabetical order will be selected, ensuring consistent
					// cross-project dependency resolution across multiple runs.
					if crossProjectMatch == "" {
						crossProjectMatch = identifier
					}
				}
			}

			// If no same-project match was found, return cross-project match as fallback.
			// See note above about non-deterministic selection when multiple matches exist.
			if crossProjectMatch != "" {
				logrus.WithFields(logrus.Fields{
					"link":    link,
					"matched": crossProjectMatch,
					"project": getProject(nameToContainer[crossProjectMatch]),
				}).Debug("Found restarting linked container via cross-project service match")

				return crossProjectMatch
			}
		}
	}

	logrus.Debug("No restarting linked container found")

	return ""
}

// getProject extracts the project name from a container's compose project label.
func getProject(c types.Container) string {
	if monitoredContainer, ok := c.(*container.Container); ok {
		if info := monitoredContainer.ContainerInfo(); info != nil && info.Config != nil {
			project := compose.GetProjectName(info.Config.Labels)
			if project != "" {
				return project
			}
		}
	}
	// Fallback to parsing from container name
	containerName := c.Name()
	if idx := strings.Index(containerName, "-"); idx > 0 {
		return containerName[:idx]
	}

	return ""
}

// parseReference parses a Docker image reference with logging.
// Logs the parsing result or error, including image details and reference type.
func parseReference(
	imageName, configImage, fallbackImage string,
	container types.Container,
) (reference.Reference, error) {
	// Set up logging with container and image details.
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"image":     imageName,
	})

	// Parse the image reference using the Docker reference library.
	normalizedRef, err := reference.ParseDockerRef(imageName)
	if err != nil {
		clog.WithError(err).
			WithField("image_name", imageName).
			Debug("Failed to parse image reference")

		return nil, fmt.Errorf("failed to parse image reference %s: %w", imageName, err)
	}

	// Log successful parsing with reference type and context.
	clog.WithFields(logrus.Fields{
		"image_name":     imageName,
		"config_image":   configImage,
		"fallback_image": fallbackImage,
		"ref_type":       fmt.Sprintf("%T", normalizedRef),
	}).Debug("Parsed image reference")

	return normalizedRef, nil
}

// isPinned checks if a container’s image is pinned by a digest reference.
//
// It selects a valid image name from ImageName(), Config.Image,
// or a fallback (imageInfo.ID or container name),
// parsing it to detect digest-based references (e.g., @sha256:...).
// If pinned, it marks the container as scanned in the progress report
// to skip updates, preserving immutability.
//
// Parameters:
//   - container: The container to check for a pinned image.
//   - progress: The progress tracker to update for scanned or skipped containers.
//   - params: Update parameters for monitor-only check.
//
// Returns:
//   - bool: True if the image is pinned by digest, false otherwise.
//   - error: Non-nil if no valid image reference can be parsed, nil on success.
func isPinned(
	container types.Container,
	progress *session.Progress,
	config types.UpdateParams,
) (bool, error) {
	// Set up logging with container and image details for debugging.
	clog := logrus.WithFields(logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	})

	// Get initial image name and configuration.
	imageName := container.ImageName()
	configImage := container.ContainerInfo().Config.Image
	fallbackImage := getFallbackImage(container)

	// Check if ImageName is invalid and fall back to Config.Image or a derived name.
	if isInvalidImageName(imageName) {
		clog.WithField("invalid_image", imageName).Debug("Invalid ImageName detected")

		if configImage != "" && !isInvalidImageName(configImage) {
			imageName = configImage
			clog.WithField("config_image", configImage).Debug("Using Config.Image as fallback")
		} else {
			imageName = fallbackImage
			clog.WithField("fallback_image", fallbackImage).Debug("Using derived fallback image")
		}
	}

	// If the final imageName is still invalid, skip the container.
	if isInvalidImageName(imageName) {
		return false, errInvalidImageReference
	}

	// Parse the selected image name to check for a digest-based reference.
	normalizedRef, err := parseReference(imageName, configImage, fallbackImage, container)
	if err != nil && imageName != fallbackImage {
		// Retry parsing with the fallback image if the initial attempt failed.
		clog.Debug("Retrying with fallback image")

		normalizedRef, err = parseReference(fallbackImage, configImage, fallbackImage, container)
	}

	if err != nil {
		return false, err
	}

	// Check if the parsed reference is digest-based (pinned).
	_, isDigested := normalizedRef.(reference.Digested)
	if isDigested {
		// Mark the container as scanned to skip updates for pinned images.
		clog.WithField("is_digested", isDigested).Debug("Pinned image detected, marking as scanned")
		progress.AddScanned(container, container.ImageID(), config)
	}

	return isDigested, nil
}

// getFallbackImage derives a fallback image name from container info.
// Uses the container name with ":latest" as the fallback image.
func getFallbackImage(container types.Container) string {
	return container.Name() + ":latest"
}

// isInvalidImageName checks if an image name is invalid.
// Returns true if the name is empty, ":latest", or starts with ":".
func isInvalidImageName(name string) bool {
	return name == "" || name == ":latest" || strings.HasPrefix(name, ":")
}

// performRollingRestart updates containers with rolling restarts.
//
// It processes containers sequentially in forward order, stopping and restarting each as needed,
// collecting cleaned image info for stale containers only to ensure proper cleanup.
// The function checks for context cancellation at the start of each iteration to enable
// prompt exit when the context is canceled.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - containers: List of containers to update or restart.
//   - client: Container client for Docker operations.
//   - config: Update options controlling restart behavior.
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info for deferred cleanup.
//   - progress: Progress tracker to update with new container IDs.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed updates.
//   - error: Non-nil if context was canceled, nil otherwise.
func performRollingRestart(
	ctx context.Context,
	containers []types.Container,
	client container.Client,
	config types.UpdateParams,
	cleanupImageInfos *[]types.RemovedImageInfo,
	progress *session.Progress,
) (map[types.ContainerID]error, error) {
	failed := make(map[types.ContainerID]error, len(containers))

	containerNames := make([]string, len(containers))
	for i, c := range containers {
		containerNames[i] = c.Name()
	}

	logrus.WithField("processing_order", containerNames).Debug("Starting performRollingRestart")

	// Process containers in forward order to respect dependency chains.
	for i := range containers {
		// Check for context cancellation to enable prompt exit when context is canceled.
		select {
		case <-ctx.Done():
			return failed, fmt.Errorf("rolling restart cancelled: %w", ctx.Err())
		default:
		}

		c := containers[i]
		if !c.ToRestart() {
			continue
		}

		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		logrus.WithFields(fields).Debug("Processing container for rolling restart")

		// Mark for update if stale
		if c.IsStale() && progress != nil {
			progress.MarkForUpdate(c.ID())
		}

		// Stop the container, handling any errors.
		err := stopStaleContainer(ctx, c, client, config)
		if err != nil {
			failed[c.ID()] = err
		} else {
			newContainerID, renamed, err := restartStaleContainer(ctx, c, client, config)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Set the new container ID in progress
				if progress != nil {
					if status, exists := (*progress)[c.ID()]; exists {
						status.SetNewContainerID(newContainerID)
						// Mark as restarted if not stale (not updated)
						if !c.IsStale() {
							progress.MarkRestarted(c.ID())
						}
					}
				}

				// Wait for the container to become healthy if it has a health check
				waitErr := client.WaitForContainerHealthy(
					ctx,
					newContainerID,
					defaultHealthCheckTimeout,
				)
				if waitErr != nil {
					logrus.WithFields(fields).
						WithError(waitErr).
						Warn("Failed to wait for container to become healthy")

					// Don't fail the update, just log the warning
				}

				if c.IsStale() && !renamed {
					// Only collect cleaned image info for stale containers that were not renamed, as renamed
					// containers (Watchtower self-updates) are cleaned up by CheckForMultipleWatchtowerInstances
					// in the new container.
					addCleanupImageInfo(
						cleanupImageInfos,
						c.ImageID(),
						c.ImageName(),
						c.Name(),
						c.ID(),
					)

					logrus.WithFields(fields).Debug("Updated container")
				}
			}
		}
	}

	return failed, nil
}

// stopContainersInReversedOrder stops containers in reverse order.
//
// It stops each container, tracking stopped images and errors, to prepare for restarts while
// respecting dependency order.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - containers: List of containers to stop.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and other behaviors.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed stops.
//   - []types.RemovedImageInfo: Slice of cleaned image info for stopped containers.
func stopContainersInReversedOrder(
	ctx context.Context,
	containers []types.Container,
	client container.Client,
	config types.UpdateParams,
) (map[types.ContainerID]error, []types.RemovedImageInfo) {
	failed := make(map[types.ContainerID]error, len(containers))
	stopped := make([]types.RemovedImageInfo, 0, len(containers))

	// Stop containers in reverse order to avoid breaking dependencies.
	for i := len(containers) - 1; i >= 0; i-- {
		c := containers[i]
		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		err := stopStaleContainer(ctx, c, client, config)
		if err != nil {
			failed[c.ID()] = err
		} else {
			stopped = append(
				stopped,
				types.RemovedImageInfo{
					ImageID:       c.ImageID(),
					ContainerID:   c.ID(),
					ImageName:     c.ImageName(),
					ContainerName: c.Name(),
				},
			)

			logrus.WithFields(fields).Debug("Stopped container")
		}
	}

	return failed, stopped
}

// stopStaleContainer stops a stale container if eligible.
//
// It skips Watchtower containers or those not marked for restart, runs pre-update hooks if enabled,
// and stops the container with the specified timeout.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - container: Container to stop.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and lifecycle hooks.
//
// Returns:
//   - error: Non-nil if stop fails, nil on success or if skipped.
func stopStaleContainer(
	ctx context.Context,
	container types.Container,
	client container.Client,
	config types.UpdateParams,
) error {
	fields := logrus.Fields{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Skip Watchtower containers to avoid self-interruption.
	if container.IsWatchtower() {
		logrus.WithFields(fields).Debug("Skipping Watchtower container")

		return nil
	}

	// Skip containers not marked for restart (e.g., not stale or linked).
	if !container.ToRestart() {
		return nil
	}

	logrus.WithFields(fields).Debug("Stopping container for restart")

	// Verify configuration for linked containers to ensure restart compatibility.
	if container.IsLinkedToRestarting() {
		err := container.VerifyConfiguration()
		if err != nil {
			logrus.WithFields(fields).
				WithError(err).
				Debug("Failed to verify container configuration")

			return fmt.Errorf("%w: %w", errVerifyConfigFailed, err)
		}
	}

	// Execute pre-update lifecycle hooks if enabled, checking for skip conditions.
	if config.LifecycleHooks {
		skipUpdate, err := lifecycle.ExecutePreUpdateCommand(
			ctx,
			client,
			container,
			config.LifecycleUID,
			config.LifecycleGID,
		)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Pre-update command execution failed")

			return fmt.Errorf("%w: %w", errPreUpdateFailed, err)
		}

		if skipUpdate {
			logrus.WithFields(fields).Debug("Skipping container due to pre-update exit code 75")

			return errSkipUpdate
		}
	}

	// Stop the container with the configured timeout.
	err := client.StopAndRemoveContainer(ctx, container, config.Timeout)
	if err != nil {
		// Check if the container is already gone (e.g., "No such container" error).
		// Treat this as non-fatal, similar to RemoveExcessWatchtowerInstances.
		if cerrdefs.IsNotFound(err) {
			logrus.WithFields(fields).
				WithError(err).
				Debug("Container not found, treating as already stopped")

			return nil
		}

		logrus.WithFields(fields).WithError(err).Error("Failed to stop container")

		return fmt.Errorf("%w: %w", errStopContainerFailed, err)
	}

	return nil
}

// restartContainersInSortedOrder restarts stopped containers.
//
// It restarts containers in dependency order, collecting cleaned image info
// for stale containers that were not renamed during a self-update, and
// tracking any restart failures.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - containers: List of containers to restart.
//   - client: Container client for Docker operations.
//   - config: Update options controlling restart behavior.
//   - stoppedImages: Slice of cleaned image info for previously stopped containers.
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info for deferred cleanup.
//   - progress: Progress tracker to update with new container IDs.
//
// Returns:
//   - map[types.ContainerID]error: Map of container IDs to errors for failed restarts.
func restartContainersInSortedOrder(
	ctx context.Context,
	containers []types.Container,
	client container.Client,
	config types.UpdateParams,
	stoppedImages []types.RemovedImageInfo,
	cleanupImageInfos *[]types.RemovedImageInfo,
	progress *session.Progress,
) map[types.ContainerID]error {
	failed := make(map[types.ContainerID]error, len(containers))
	// Track renamed containers to skip cleanup.
	renamedContainers := make(map[types.ContainerID]bool)

	// Restart containers in sorted order to respect dependency chains.
	for _, c := range containers {
		if !c.ToRestart() {
			continue
		}

		fields := logrus.Fields{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		// Check if container was previously stopped by looking in stoppedImages slice.
		wasStopped := false

		for _, sc := range stoppedImages {
			if sc.ContainerID == c.ID() {
				wasStopped = true

				break
			}
		}

		// Skip other Watchtower containers from self-updates
		if c.IsWatchtower() && config.CurrentContainerID != "" &&
			c.ID() != config.CurrentContainerID {
			continue
		}

		// Restart Watchtower containers regardless of stoppedImages, as they are renamed.
		// Otherwise, restart only containers that were previously stopped.
		if c.IsWatchtower() || wasStopped {
			newContainerID, renamed, err := restartStaleContainer(ctx, c, client, config)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Set the new container ID in progress
				if progress != nil {
					if status, exists := (*progress)[c.ID()]; exists {
						status.SetNewContainerID(newContainerID)
						// Mark as restarted if not stale (not updated)
						if !c.IsStale() {
							progress.MarkRestarted(c.ID())
						}
					}
				}

				logrus.WithFields(fields).Debug("Restarted container")

				if renamed {
					renamedContainers[c.ID()] = true
				}
				// Only collect cleaned image info for stale containers that were not renamed, as renamed
				// containers (Watchtower self-updates) are cleaned up by CheckForMultipleWatchtowerInstances
				// in the new container.
				if c.IsStale() && !renamedContainers[c.ID()] {
					addCleanupImageInfo(
						cleanupImageInfos,
						c.ImageID(),
						c.ImageName(),
						c.Name(),
						c.ID())
				}
			}
		}
	}

	return failed
}

// addCleanupImageInfo adds cleanup info if not already present.
//
// Parameters:
//   - cleanupImageInfos: Pointer to slice to collect cleaned image info.
//   - imageID: ID of the image to clean up.
//   - imageName: Name of the image.
//   - containerName: Name of the container.
//   - containerID: ID of the container (optional, pass empty string if not available).
func addCleanupImageInfo(
	cleanupImageInfos *[]types.RemovedImageInfo,
	imageID types.ImageID,
	imageName, containerName string,
	containerID types.ContainerID,
) {
	for _, existing := range *cleanupImageInfos {
		if existing.ImageID == imageID {
			return
		}
	}

	*cleanupImageInfos = append(*cleanupImageInfos, types.RemovedImageInfo{
		ImageID:       imageID,
		ContainerID:   containerID,
		ImageName:     imageName,
		ContainerName: containerName,
	})
}

// restartStaleContainer restarts a stale container.
//
// It renames Watchtower containers if applicable, starts a new container,
// and runs post-update hooks.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - sourceContainer: Container to restart.
//   - client: Container client for Docker operations.
//   - config: Update options controlling restart and lifecycle hooks.
//
// Returns:
//   - types.ContainerID: ID of the new container if started, original ID if renamed only, empty otherwise.
//   - bool: True if the container was renamed, false otherwise.
//   - error: Non-nil if restart fails, nil on success.
func restartStaleContainer(
	ctx context.Context,
	sourceContainer types.Container,
	client container.Client,
	config types.UpdateParams,
) (types.ContainerID, bool, error) {
	fields := logrus.Fields{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	}

	renamed := false
	newContainerID := sourceContainer.ID() // Default to original ID

	// Rename Watchtower containers regardless of NoRestart flag,
	// but skip in run-once mode as there's no need to avoid conflicts
	// with a continuously running instance.
	if sourceContainer.IsWatchtower() && !config.RunOnce {
		newName := "watchtower-old-" + sourceContainer.ID().ShortID()

		err := client.RenameContainer(ctx, sourceContainer, newName)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"container": sourceContainer.Name(),
				"new_name":  newName,
			}).Debug("Failed to rename Watchtower container")

			return "", false, fmt.Errorf("%w: %w", errRenameWatchtowerFailed, err)
		}

		logrus.WithFields(fields).
			WithField("new_name", newName).
			Debug("Renamed Watchtower container")

		renamed = true
	}

	// For Watchtower self-updates, accumulate container ID chain in labels.
	if sourceContainer.IsWatchtower() {
		if c, ok := sourceContainer.(*container.Container); ok {
			containerInfo := c.ContainerInfo()
			if containerInfo != nil && containerInfo.Config != nil {
				existingChain, _ := c.GetContainerChain()

				var newChain string
				if existingChain != "" {
					newChain = existingChain + "," + string(c.ID())
				} else {
					newChain = string(c.ID())
				}

				if containerInfo.Config.Labels == nil {
					containerInfo.Config.Labels = make(map[string]string)
				}

				containerInfo.Config.Labels[container.ContainerChainLabel] = newChain
				logrus.WithFields(fields).
					WithField("container_chain", newChain).
					Debug("Updated container chain label for Watchtower self-update")
			}
		}
	}

	// Start the new container unless restarts are disabled.
	// Watchtower containers are always started.
	if !config.NoRestart || sourceContainer.IsWatchtower() {
		logrus.WithFields(fields).Debug("Starting container with updated configuration")

		var err error

		// Start the new container.
		newContainerID, err = client.StartContainer(ctx, sourceContainer)
		if err != nil {
			logrus.WithFields(fields).WithError(err).Debug("Failed to start container")

			// If there's an error and the container is an old Watchtower container,
			// then stop and remove it.
			if renamed && sourceContainer.IsWatchtower() {
				logrus.WithFields(fields).Debug("Cleaning up failed Watchtower container")

				cleanupErr := client.StopAndRemoveContainer(ctx, sourceContainer, config.Timeout)
				if cleanupErr != nil {
					logrus.WithError(cleanupErr).
						WithFields(fields).
						Debug("Failed to stop failed Watchtower container")
				}
			}

			return "", renamed, fmt.Errorf("%w: %w", errStartContainerFailed, err)
		}

		// Run post-update lifecycle hooks for restarting containers if enabled.
		if sourceContainer.ToRestart() && config.LifecycleHooks {
			logrus.WithFields(fields).Debug("Executing post-update command")
			lifecycle.ExecutePostUpdateCommand(
				ctx,
				client,
				newContainerID,
				config.LifecycleUID,
				config.LifecycleGID,
			)
		}
	}

	// For renamed Watchtower containers, update restart policy to "no" to prevent auto-restart.
	if renamed && sourceContainer.IsWatchtower() {
		logrus.WithFields(fields).
			Debug("Updating restart policy for old Watchtower container")

		// Create configuration update
		updateConfig := dockerContainer.UpdateConfig{
			RestartPolicy: dockerContainer.RestartPolicy{
				Name: "no",
			},
		}
		// Update the renamed Watchtower container's restart policy.
		err := client.UpdateContainer(ctx, sourceContainer, updateConfig)
		if err != nil {
			logrus.WithError(err).
				WithFields(fields).
				Warn("Failed to update restart policy for old Watchtower container")
		}
	}

	return newContainerID, renamed, nil
}
