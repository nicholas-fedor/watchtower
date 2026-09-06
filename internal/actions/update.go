package actions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/distribution/reference"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/nicholas-fedor/watchtower/pkg/compose"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/lifecycle"
	"github.com/nicholas-fedor/watchtower/pkg/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	// defaultPullFailureDelay defines the default delay duration for failed Watchtower self-update pulls.
	defaultPullFailureDelay = 5 * time.Minute

	// defaultHealthCheckTimeout defines the default timeout for waiting for container health checks.
	defaultHealthCheckTimeout = 5 * time.Minute

	// defaultRestartPolicyTimeout defines the fallback timeout for restart policy
	// updates when config.Timeout is non-positive.
	defaultRestartPolicyTimeout = 30 * time.Second

	// defaultCreateStartTimeout defines the minimum timeout for container create
	// and start operations during restart. It guards against slow hosts where
	// Docker API calls can exceed the default 30s restart policy timeout.
	defaultCreateStartTimeout = 5 * time.Minute
)

// isRecoverableOrphan determines whether a container is a recoverable orphaned
// Watchtower instance. It excludes the current container, old-container names,
// non-Watchtower containers, containers that are not in the created state, and
// containers from a different scope.
//
// Parameters:
//   - c: Candidate container to evaluate.
//   - currentContainer: The current running container to exclude.
//   - currentScope: Normalized scope of the current container ("none" when unset).
//
// Returns:
//   - bool: True if the candidate is a recoverable orphan, false otherwise.
func isRecoverableOrphan(
	c types.Container,
	currentContainer types.Container,
	currentScope string,
) bool {
	if !c.IsWatchtower() {
		return false
	}

	if c.ID() == currentContainer.ID() {
		return false
	}

	if container.IsOldContainer(c.Name()) {
		return false
	}

	if !c.IsCreated() {
		return false
	}

	candidateScope, candidateHasScope := c.Scope()
	if !candidateHasScope || candidateScope == "" {
		candidateScope = "none"
	}

	if candidateScope != currentScope {
		return false
	}

	return true
}

// TryRecoverOrphanedContainer attempts to start an orphaned Watchtower container
// that is stuck in the Docker "created" state. This can happen when a self-update
// creates a replacement container but fails to start it, leaving the old container
// running and the new container never started.
//
// It lists all containers, filters for Watchtower containers in the created state
// that are not the current container, and attempts to start the first match.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker API operations.
//   - currentContainer: The current running container to exclude from recovery.
//
// Returns:
//   - types.Container: The recovered container if successful, nil otherwise.
//   - bool: True if a container was found and started, false otherwise.
func TryRecoverOrphanedContainer(log *zerolog.Logger, ctx context.Context,
	client container.Client,
	currentContainer types.Container,
) (types.Container, bool) {
	if currentContainer == nil {
		return nil, false
	}

	allContainers, err := client.ListContainers(ctx, filters.NoFilter)
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Failed to list containers for orphaned Watchtower recovery")

		return nil, false
	}

	currentScope, currentHasScope := currentContainer.Scope()
	if !currentHasScope || currentScope == "" {
		currentScope = "none"
	}

	for _, c := range allContainers {
		if !isRecoverableOrphan(c, currentContainer, currentScope) {
			continue
		}

		log.Info().
			Str("container_id", string(c.ID())).
			Str("container_name", c.Name()).
			Msg("Attempting to recover orphaned Watchtower container")

		err := client.StartContainerByID(ctx, c.ID())
		if err != nil {
			log.Debug().
				Err(err).
				Str("container_id", string(c.ID())).
				Str("container_name", c.Name()).
				Msg("Failed to start orphaned Watchtower container")

			continue
		}

		log.Info().
			Str("container_id", string(c.ID())).
			Str("container_name", c.Name()).
			Msg("Successfully recovered orphaned Watchtower container")

		return c, true
	}

	return nil, false
}

// Update scans and updates containers based on parameters.
//
// It checks container staleness, sorts by dependencies, and updates or restarts
// containers as needed, collecting cleaned image info for cleanup.
// Non-stale linked containers are restarted but not marked as updated.
// Containers with pinned images (referenced by digest) are skipped to
// preserve immutability.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for interacting with Docker API.
//   - config: UpdateParams specifying behavior like cleanup, restart, and filtering.
//
// Returns:
//   - types.Report: Session report summarizing scanned, updated, and failed containers.
//   - []types.RemovedImageInfo: Slice of cleaned image info to clean up after updates.
//   - error: Non-nil if listing or sorting fails, nil on success.
func Update(log *zerolog.Logger, ctx context.Context,
	client container.Client,
	config types.UpdateParams,
) (types.Report, []types.RemovedImageInfo, error) {
	// Check for context cancellation early
	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("update canceled: %w", ctx.Err())
	default:
	}

	// Initialize logging for the update process start.
	log.Debug().Msg("Starting container update check")

	err := checkImageDiskSpace(log, ctx, client, config)
	if err != nil {
		return nil, nil, err
	}

	// Fetch all containers for monitoring
	allContainers, err := client.ListContainers(
		ctx,
		filters.NoFilter,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Initialize a slice to collect cleaned image info for cleanup after updates.
	cleanupImageInfos := []types.RemovedImageInfo{}

	// Self-check: if the current container is an old Watchtower container,
	// update its restart policy to "no" and exit to prevent Docker from
	// restarting it. This catches cases where the startup check was bypassed
	// or the container was restarted despite other safeguards.
	if config.CurrentContainerID != "" {
		for _, c := range allContainers {
			if c.ID() == config.CurrentContainerID && c.IsWatchtower() {
				if container.IsOldContainer(c.Name()) {
					log.Debug().
						Str("container", c.Name()).
						Msg("Current container is an old Watchtower container, stopping self")

					recoverCtx, recoverCancel := context.WithTimeout(
						context.Background(),
						restartPolicyTimeout(config.Timeout),
					)
					defer recoverCancel()

					//nolint:contextcheck // recoverCtx is intentionally created with timeout and passed to recovery
					recoveredContainer, recovered := TryRecoverOrphanedContainer(log,
						recoverCtx,
						client,
						c,
					)
					if recovered {
						log.Info().
							Str("container", recoveredContainer.Name()).
							Msg("Recovered orphaned Watchtower container, exiting old instance")
					}

					setNoRestartCtx, setNoRestartCancel := context.WithTimeout(
						context.Background(),
						restartPolicyTimeout(config.Timeout),
					)
					defer setNoRestartCancel()

					//nolint:contextcheck // setNoRestartCtx is intentionally used for restart policy update
					client.SetNoRestartPolicy(setNoRestartCtx, c)

					return nil, nil, errOldSelfDetected
				}

				break
			}
		}
	}

	// Clean up any old Watchtower containers that linger from a previous
	// self-update. This runs each update cycle to catch containers that the
	// startup cleanup may have missed (e.g., they were still stopping at startup).
	// Scope is derived from the current container to avoid crossing scope boundaries.
	// When the current container isn't found in the list, cleanup is
	// skipped to prevent accidentally removing old containers from other
	// scoped instances. Unscoped containers ("") are normalized to "none".
	if config.CurrentContainerID != "" {
		currentScope, found := deriveScopeFromCurrentContainer(log,
			allContainers,
			config.CurrentContainerID,
		)

		if !found {
			log.Debug().Msg("Skipping old container cleanup: current container not found in list")
		} else {
			if currentScope == "" {
				currentScope = "none"
			}

			_, cleanupErr := CleanupOldWatchtowerContainers(log,
				ctx,
				client,
				config.Cleanup,
				currentScope,
				config.CurrentContainerID,
				&cleanupImageInfos,
			)
			if cleanupErr != nil {
				log.Warn().
					Err(cleanupErr).
					Msg("Failed to clean up old Watchtower containers, continuing update cycle")
			}
		}
	}

	// Create a progress tracker for reporting scanned, updated, and skipped containers.
	progress := &session.Progress{}
	// Track the number of stale containers for logging.
	var staleCount int
	// Track if Watchtower self-update pull failed to add safeguard delay.
	var watchtowerPullFailed bool

	// Filter containers based on the provided filter (e.g., all, specific names).
	// Use a non-nil empty slice so lifecycle hooks can distinguish a valid empty
	// snapshot from an uninitialized list.
	filteredContainers := make([]types.Container, 0, len(allContainers))

	for _, c := range allContainers {
		if config.Filter == nil || config.Filter(c) {
			filteredContainers = append(filteredContainers, c)
		}
	}

	// Run pre-check lifecycle hooks if enabled to validate the environment before updates.
	if config.LifecycleHooks {
		log.Debug().Msg("Executing pre-check lifecycle hooks")
		lifecycle.ExecutePreChecks(log, ctx, client, config, filteredContainers)
	}

	// Prepare a list of container names and images for detailed debugging output.
	filteredContainerNames := make([]string, len(filteredContainers))
	for i, c := range filteredContainers {
		filteredContainerNames[i] = fmt.Sprintf(
			"%s (%s)",
			c.Name(),
			c.ImageName(),
		)
	}
	// Log the retrieved containers and filter details.
	log.Debug().
		Int("count", len(filteredContainers)).
		Strs("containers", filteredContainerNames).
		Str("filter", fmt.Sprintf("%T", config.Filter)).
		Msg("Retrieved containers for update check")

	// Skip monitored containers that reference themselves as dependencies
	// via the Watchtower depends-on label.
	for _, monitoredContainer := range filteredContainers {
		if hasSelfDependency(log, monitoredContainer) {
			progress.AddSkipped(log,
				monitoredContainer,
				errSelfDependency,
				config,
			)
			log.Warn().
				Str("container", monitoredContainer.Name()).
				Str("id", monitoredContainer.ID().ShortID()).
				Msg("Skipping container update (self-dependency)")
		}
	}

	// Detect circular dependencies and mark affected containers as skipped.
	cycles := container.DetectCycles(
		filteredContainers,
		config.UseComposeDependsOn,
	)
	for _, c := range filteredContainers {
		if cycles[container.ResolveContainerIdentifier(c)] {
			progress.AddSkipped(log, c, errCircularDependency, config)
			log.Warn().
				Str("container", c.Name()).
				Str("id", c.ID().ShortID()).
				Msg("Skipping container update (circular dependency)")
		}
	}

	// Track containers that fail staleness checks for reporting.
	staleCheckFailed := 0

	// Prepare containers for staleness checks, skipping already-processed ones.
	type checkTask struct {
		index     int
		container types.Container
	}

	var checkTasks []checkTask

	// Iterate through containers to check staleness and prepare for updates or restarts.
	for i, sourceContainer := range filteredContainers {
		// Check for context cancellation to enable faster shutdown during long update cycles.
		select {
		case <-ctx.Done():
			return progress.Report(log), cleanupImageInfos, ctx.Err()
		default:
		}

		// Skip containers already processed (e.g., skipped due to circular dependencies).
		_, exists := (*progress)[sourceContainer.ID()]
		if exists {
			continue
		}

		// Set up logging fields for the current container.
		clogVal := log.With().
			Str("container", sourceContainer.Name()).
			Str("image", sourceContainer.ImageName()).
			Logger()
		clog := &clogVal

		// Check if the container uses a pinned (digest-based) image to skip updates.
		isPinnedVal, err := isPinned(log, sourceContainer, progress, config)
		if err != nil {
			// Log and skip containers with unparsable image references, marking as skipped.
			clog.Debug().
				Err(err).
				Msg("Failed to check pinned image - skipping container")

			progress.AddSkipped(log,
				sourceContainer,
				fmt.Errorf("%w: %w", errParseImageReference, err),
				config,
			)

			staleCheckFailed++

			continue
		}

		if isPinnedVal {
			// Skip staleness checks for pinned images and mark as scanned.
			clog.Debug().Msg("Skipping staleness check for pinned image")

			continue
		}

		checkTasks = append(checkTasks, checkTask{
			index:     i,
			container: sourceContainer,
		})
	}

	// Check for context cancellation before launching parallel staleness checks.
	select {
	case <-ctx.Done():
		return progress.Report(log), cleanupImageInfos, ctx.Err()
	default:
	}

	// Parallelize staleness checks with bounded concurrency.
	const maxConcurrentChecks = 20

	var checkGroup errgroup.Group
	checkGroup.SetLimit(maxConcurrentChecks)

	var (
		resultMu                     sync.Mutex
		parallelStaleCheckFailed     int
		parallelWatchtowerPullFailed bool
		parallelStaleCount           int
	)

	for _, task := range checkTasks {
		checkGroup.Go(func() error {
			// Check for context cancellation to enable faster shutdown during long update cycles.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			sourceContainer := task.container
			clogVal := log.With().
				Str("container", sourceContainer.Name()).
				Str("image", sourceContainer.ImageName()).
				Logger()
			clog := &clogVal

			var (
				stale       bool
				newestImage types.ImageID
				checkErr    error
				verifyErr   error
			)

			// Determine if the container is stale and needs updating.
			// If the container is Watchtower and SkipSelfUpdate is enabled, skip the update
			// by setting stale to false and using the current image. Otherwise, check staleness.
			if sourceContainer.IsWatchtower() && config.SkipSelfUpdate {
				stale = false
				newestImage = sourceContainer.ImageID()
			} else {
				stale, newestImage, _, checkErr = client.IsContainerStale(
					ctx,
					sourceContainer,
					config,
				)

				if checkErr != nil && (errors.Is(checkErr, context.Canceled) || errors.Is(checkErr, context.DeadlineExceeded)) {
					return fmt.Errorf("staleness check canceled: %w", checkErr)
				}
			}

			// Determine if the container should be updated based on staleness and config.
			shouldUpdate := shouldUpdateContainer(sourceContainer, stale, config)

			// Log when skipping Watchtower self-update in run-once mode.
			if stale && sourceContainer.IsWatchtower() && config.RunOnce {
				clog.Info().Msg("Skipping Watchtower self-update in run-once mode")
			}

			// Verify the container's configuration if it's slated for update to
			// ensure recreation is possible.
			if checkErr == nil && shouldUpdate {
				verifyErr = sourceContainer.VerifyConfiguration()
				if verifyErr != nil {
					clog.Debug().
						Err(verifyErr).
						Str("container_id", sourceContainer.ID().ShortID()).
						Str("image_id", sourceContainer.ImageID().ShortID()).
						Msg("Failed to verify container configuration")
				}

				if verifyErr != nil &&
					(errors.Is(verifyErr, context.Canceled) ||
						errors.Is(verifyErr, context.DeadlineExceeded)) {
					return fmt.Errorf("configuration verification canceled: %w", verifyErr)
				}
			}

			resultMu.Lock()
			defer resultMu.Unlock()

			// Handle staleness check results, logging skips or adding to the progress report.
			switch {
			case checkErr != nil:
				// Skip containers with staleness check errors, marking them as skipped.
				if !errors.Is(checkErr, container.ErrImageCooldown) {
					parallelStaleCheckFailed++
				}

				if ratelimit.Is(checkErr) {
					progress.AddFailed(log, sourceContainer, checkErr, config)
				} else {
					progress.AddSkipped(log, sourceContainer, checkErr, config)
				}

				// Restore rich cooldown metadata for reports/notifications (preserves the
				// structured CooldownAge/Delay/Remaining/Passed fields that the removed
				// high-level block used to populate via SetCooldownInfo). The rich
				// *container.CooldownError carries the details.
				cooldownErr, ok := errors.AsType[*container.CooldownError](checkErr)
				if ok {
					progress.SetCooldownInfo(log,
						sourceContainer.ID(),
						cooldownErr.Age,
						cooldownErr.Delay,
						cooldownErr.Remaining,
						cooldownErr.EligibleAt,
						cooldownErr.Passed,
					)
				} else if errors.Is(checkErr, container.ErrImageCooldown) {
					// Fallback for plain sentinel (keeps basic deferral visible)
					progress.SetCooldownInfo(log, sourceContainer.ID(), "", "", "", time.Time{}, false)
				}

				// Track if Watchtower self-update pull failed for safeguard.
				// Only set to true if we actually attempted a self-update
				// (i.e., SkipSelfUpdate is false) and the error is a real
				// failure, not a cooldown deferral.
				if sourceContainer.IsWatchtower() &&
					!config.SkipSelfUpdate &&
					!errors.Is(checkErr, container.ErrImageCooldown) {
					parallelWatchtowerPullFailed = true
				}
			case verifyErr != nil:
				parallelStaleCheckFailed++

				progress.AddSkipped(log, sourceContainer, verifyErr, config)

				if sourceContainer.IsWatchtower() &&
					!config.SkipSelfUpdate &&
					!errors.Is(verifyErr, container.ErrImageCooldown) {
					parallelWatchtowerPullFailed = true
				}
			default:
				// For fresh containers, set newestImage to current image ID for proper categorization.
				// (Cooldown decision and any layer pull now happen inside pkg/container/image.go
				// IsOutsideCooldown + guarded PullImage, after digest staleness and before layers.)
				if !stale {
					newestImage = sourceContainer.ImageID()
				}

				// Log successful staleness check and add to scanned containers.
				clog.Debug().
					Bool("stale", stale).
					Str("newest_image", string(newestImage)).
					Msg("Checked container staleness")
				progress.AddScanned(log,
					sourceContainer,
					newestImage,
					config,
				)
			}

			// Track old image ID before update for cleanup notifications.
			if shouldUpdate {
				c, ok := filteredContainers[task.index].(*container.Container)
				if ok {
					c.SetOldImageID(sourceContainer.ImageID())
				}
			}

			// Update the container's stale status for dependency sorting.
			// Only mark as stale if the container should actually be updated.
			filteredContainers[task.index].SetStale(stale && shouldUpdate && checkErr == nil && verifyErr == nil)

			// Increment stale count for logging summary.
			if stale {
				parallelStaleCount++
			}

			return nil
		})
	}

	err = checkGroup.Wait()
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Parallel staleness checks completed with error")

		// Surface context cancellation from parallel workers as the final Update error.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return progress.Report(log), cleanupImageInfos, fmt.Errorf("update canceled: %w", err)
		}
	}

	// Accumulate parallel results with pre-parallel sequential counts so failures
	// from pinned-image parsing and other pre-filter checks are preserved.
	staleCheckFailed += parallelStaleCheckFailed
	watchtowerPullFailed = watchtowerPullFailed || parallelWatchtowerPullFailed
	staleCount += parallelStaleCount

	// Log the summary of staleness checks, including total, stale, and failed counts.
	log.Debug().
		Int("total", len(filteredContainers)).
		Int("stale", staleCount).
		Int("failed", staleCheckFailed).
		Msg("Completed container staleness check")

	// Build a map for a lookup of containers by ID.
	containerByID := make(map[types.ContainerID]types.Container, len(allContainers))
	for _, ac := range allContainers {
		containerByID[ac.ID()] = ac
	}

	// Propagate stale status to allContainers since they are different instances.
	for _, c := range filteredContainers {
		if c.IsStale() {
			ac, ok := containerByID[c.ID()]
			if ok {
				ac.SetStale(true)
			}
		}
	}

	// Sort containers by dependencies to ensure correct update and restart order.
	err = sorter.SortByDependencies(log,
		filteredContainers,
		config.UseComposeDependsOn,
	)
	if err != nil {
		if errors.Is(err, sorter.ErrCircularReference) {
			circularErr, ok := errors.AsType[sorter.CircularReferenceError](err)
			if ok {
				circularName := circularErr.ContainerName
				// Find the container and mark as skipped.
				for _, c := range filteredContainers {
					if c.Name() == circularName {
						// Only add if not already skipped (e.g., from initial cycle detection)
						_, exists := (*progress)[c.ID()]
						if !exists {
							progress.AddSkipped(log,
								c,
								errCircularDependency,
								config,
							)
							log.Warn().
								Str("container", c.Name()).
								Str("id", c.ID().ShortID()).
								Msg("Skipping container update (circular dependency)")
						}

						break
					}
				}
			}
			// Skip UpdateImplicitRestart to avoid potential issues with circular dependencies.
		} else {
			// Log and return an error if dependency sorting fails for other reasons.
			log.Debug().
				Err(err).
				Msg("Failed to sort containers by dependencies")

			return nil, []types.RemovedImageInfo{}, fmt.Errorf(
				"%w: %w",
				errSortDependenciesFailed,
				err,
			)
		}
	} else {
		// Mark containers linked to restarting ones for restart without updating.
		UpdateImplicitRestart(log,
			allContainers,
			filteredContainers,
			config.UseComposeDependsOn,
		)
	}

	// Collect all containers to restart (updates and implicit restarts)
	var allContainersToRestart []types.Container

	for _, c := range filteredContainers {
		if c.ToRestart() && !c.IsMonitorOnly(config) {
			allContainersToRestart = append(allContainersToRestart, c)
		}
	}

	// Sort containers to restart by dependencies to ensure correct update and restart order.
	err = sorter.SortByDependencies(log,
		allContainersToRestart,
		config.UseComposeDependsOn,
	)
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Failed to sort all containers to restart by dependencies")

		return nil, []types.RemovedImageInfo{}, fmt.Errorf(
			"%w: %w",
			errSortDependenciesFailed,
			err,
		)
	}

	// Log the number of containers prepared for restart.
	log.Debug().
		Int("restart_count", len(allContainersToRestart)).
		Msg("Prepared containers for restart")

	// Perform updates and restarts, either with rolling restarts or in batches.
	var (
		failedStop    map[types.ContainerID]error
		stoppedImages []types.RemovedImageInfo
		failedStart   map[types.ContainerID]error
	)

	if config.RollingRestart {
		// Apply rolling restarts for all containers in dependency order.
		rollingFailed, rollingErr := performRollingRestart(log,
			ctx,
			allContainersToRestart,
			client,
			config,
			&cleanupImageInfos,
			progress,
		)
		progress.UpdateFailed(log, rollingFailed)

		if rollingErr != nil {
			return progress.Report(log), cleanupImageInfos, rollingErr
		}
	} else {
		// Mark containers to update for update in progress
		for _, c := range allContainersToRestart {
			if c.IsStale() {
				progress.MarkForUpdate(log, c.ID())
			}
		}

		// Stop and restart containers in batches, respecting dependency order.
		failedStop, stoppedImages = stopContainersInReversedOrder(log,
			ctx,
			allContainersToRestart,
			client,
			config,
		)
		progress.UpdateFailed(log, failedStop)

		failedStart = restartContainersInSortedOrder(log,
			ctx,
			allContainersToRestart,
			client,
			config,
			stoppedImages,
			&cleanupImageInfos,
			progress,
		)
		progress.UpdateFailed(log, failedStart)
	}

	// Run post-check lifecycle hooks if enabled to finalize the update process.
	if config.LifecycleHooks {
		log.Debug().Msg("Executing post-check lifecycle hooks")
		lifecycle.ExecutePostChecks(log, ctx, client, config, filteredContainers)
	}

	// Add safeguard delay if Watchtower self-update pull failed
	// to prevent rapid restarts.
	if watchtowerPullFailed {
		delay := config.PullFailureDelay
		if delay == 0 {
			delay = defaultPullFailureDelay // Default delay
		}

		log.Info().
			Dur("delay", delay).
			Msg("Watchtower self-update pull failed - sleeping to prevent rapid restarts")

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			log.Debug().
				Err(ctx.Err()).
				Msg("Context canceled during pull-failure delay - skipping remaining delay")
		}
	}

	// Return the final report summarizing the session and the cleanup image infos.
	return progress.Report(log), cleanupImageInfos, nil
}

// hasSelfDependency checks if a container has a self-dependency in its Watchtower depends-on label.
// It now uses the shared GetLinksFromWatchtowerLabel helper function for parsing and normalization.
func hasSelfDependency(log *zerolog.Logger, c types.Container) bool {
	sourceContainer, ok := c.(*container.Container)
	if !ok {
		return false
	}

	linkLog := log.With().Str("container", c.Name()).Logger()
	links := container.GetLinksFromWatchtowerLabel(
		sourceContainer,
		&linkLog,
	)

	return slices.Contains(links, c.Name())
}

// UpdateImplicitRestart marks containers linked to restarting ones.
//
// It uses a multi-pass algorithm to ensure transitive propagation through the dependency chain,
// continuing until no more containers are marked for restart.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - allContainers: Full list of containers being managed.
//   - containers: Slice of containers to evaluate and potentially mark for restart.
//   - useComposeDependsOn: Whether to consider Docker Compose depends_on labels.
//
// This function mutates the ToRestart / LinkedToRestarting state on containers in place.
func UpdateImplicitRestart(log *zerolog.Logger, allContainers,
	containers []types.Container,
	useComposeDependsOn bool,
) {
	log.Debug().Msg("Starting UpdateImplicitRestart")

	byID := make(map[types.ContainerID]types.Container, len(allContainers))

	// Key restart tracking by the canonical identifier (ResolveContainerIdentifier)
	// so that links produced by c.Links() can be matched directly. We also record
	// the bare .Name() as an alias to improve exact-match resilience across
	// different naming conventions (explicit container_name vs. Compose defaults,
	// with or without replica suffixes).
	restartByIdentifier := make(map[string]bool, len(allContainers)+len(allContainers))

	for _, c := range allContainers {
		byID[c.ID()] = c

		// Fall back through Name then ID to guarantee a non-empty key.
		resolvedID := container.ResolveContainerIdentifier(c)
		if resolvedID == "" {
			resolvedID = c.Name()
		}

		if resolvedID == "" {
			resolvedID = string(c.ID())
		}

		if resolvedID == "" {
			log.Debug().
				Str("container_id", string(c.ID())).
				Msg("Skipping container with empty identifier")

			continue
		}

		restartByIdentifier[resolvedID] = c.ToRestart()

		// Also index by bare name for better exact matching in mixed scenarios
		bareName := c.Name()
		if bareName != "" && bareName != resolvedID {
			_, exists := restartByIdentifier[bareName]
			if !exists {
				restartByIdentifier[bareName] = c.ToRestart()
			}
		}
	}

	markedContainers := []string{}
	changed := true

	for changed {
		changed = false

		for i, c := range containers {
			if c.ToRestart() {
				continue // Skip already marked containers.
			}

			links := c.Links(useComposeDependsOn)

			containerIdentifier := container.ResolveContainerIdentifier(c)
			log.Debug().
				Str("container", c.Name()).
				Str("container_identifier", containerIdentifier).
				Strs("links", links).
				Bool("to_restart", c.ToRestart()).
				Msg("Checking links for container")

			link := linkedIdentifierMarkedForRestart(log,
				links,
				restartByIdentifier,
				c,
				allContainers,
			)
			if link != "" {
				log.Debug().
					Str("container", c.Name()).
					Str("restarting", link).
					Msg("Marked container as linked to restarting")
				containers[i].SetLinkedToRestarting(true)

				allContainer, ok := byID[c.ID()]
				if ok {
					allContainer.SetLinkedToRestarting(true)
					resolved := container.ResolveContainerIdentifier(allContainer)
					restartByIdentifier[resolved] = true

					bareName := allContainer.Name()
					if bareName != "" && bareName != resolved {
						restartByIdentifier[bareName] = true
					}

					restartByIdentifier[string(allContainer.ID())] = true
				}

				markedContainers = append(markedContainers, c.Name())
				changed = true
			}
		}
	}

	log.Debug().
		Strs("marked_containers", markedContainers).
		Msg("Completed UpdateImplicitRestart")
}

// shouldUpdateContainer determines if a container should be updated
// based on its staleness and update parameters.
//
// It checks multiple conditions:
//   - The container must be stale (outdated image)
//   - The container must not be monitor-only
//   - Watchtower containers are skipped in run-once mode
//   - Watchtower self-updates are skipped if SkipSelfUpdate is true
//
// Parameters:
//   - container: The container to check.
//   - stale: Whether the container's image is outdated.
//   - config: Update parameters controlling update behavior.
//
// Returns:
//   - bool: True if the container should be updated, false otherwise.
func shouldUpdateContainer(
	container types.Container,
	stale bool,
	config types.UpdateParams,
) bool {
	// Must be stale to update
	if !stale {
		return false
	}

	// Skip old Watchtower containers — they are predecessors from a
	// self-update and should only be removed, never updated.
	if filters.IsOldWatchtower(container) {
		return false
	}

	// Skip monitor-only containers
	if container.IsMonitorOnly(config) {
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

// linkedIdentifierMarkedForRestart returns the identifier of a container in the
// links list that is marked for restart, or the empty string if none is found.
//
// The function attempts an exact match first, then delegates to
// FindMatchingIdentifiers (exact/replica/service-only strategies) with
// same-project preference. A hyphenated link is only treated as an explicit
// project-qualified reference (restricting pure service-only results) when
// it begins with a known project prefix from the container set. Bare service
// names, including hyphenated ones, reach the service-only fallback.
//
// Parameters:
//   - links: List of linked container identifiers (from Container.Links()).
//   - restartByIdentifier: Map of container identifiers to their restart status.
//   - dependentContainer: The container whose links are being evaluated.
//   - allContainers: Full list of containers (used for project lookup and resolution).
//
// Returns:
//   - string: Identifier of a restarting linked container, or "" if none found.
func linkedIdentifierMarkedForRestart(log *zerolog.Logger, links []string,
	restartByIdentifier map[string]bool,
	dependentContainer types.Container,
	allContainers []types.Container,
) string {
	// Build a lookup that supports both bare names and ResolveContainerIdentifier
	// results, since the restart map may be populated with either.
	idToContainer := make(map[string]types.Container, len(allContainers)+len(allContainers))
	for _, c := range allContainers {
		idToContainer[c.Name()] = c

		resolved := container.ResolveContainerIdentifier(c)
		if resolved != "" && resolved != c.Name() {
			idToContainer[resolved] = c
		}
	}

	dependentProject := getProject(log, dependentContainer)

	// Collect known projects so we can distinguish bare service-name references
	// (which may contain hyphens) from explicit project-qualified identifiers
	// the caller wrote in a label.
	knownProjects := map[string]bool{}

	for _, c := range allContainers {
		p := getProject(log, c)
		if p != "" {
			knownProjects[p] = true
		}
	}

	log.Debug().
		Strs("links", links).
		Interface("restartByIdentifier", restartByIdentifier).
		Str("dependentProject", dependentProject).
		Msg("Searching for restarting linked container")

	// Overall strategy per link:
	//   1. Exact match against the restart map.
	//   2. Delegate to FindMatchingIdentifiers (exact / replica / service-only).
	//   3. Among matches, prefer same-project (via labels).
	//   4. Only treat a hyphenated link as an explicit project-qualified reference
	//      (and therefore restrict pure service-only resolution) when the link
	//      begins with a known project prefix. Bare service names are allowed to
	//      use the service fallback even when they contain hyphens.
	for _, link := range links {
		log.Debug().
			Str("checking_link", link).
			Msg("Checking link for restarting match")

		// Determine once per link whether it is written as an explicit
		// project-qualified identifier (starts with a known project- prefix).
		// Bare service names, even when they contain hyphens, must be allowed
		// to reach the service-only fallback.
		isExplicitProjectRef := false

		if strings.Contains(link, "-") {
			for p := range knownProjects {
				if strings.HasPrefix(link, p+"-") {
					isExplicitProjectRef = true

					break
				}
			}
		}

		if restartByIdentifier[link] {
			log.Debug().
				Str("found_restarting_identifier", link).
				Msg("Found restarting linked container via exact match")

			return link
		}

		// Collect only the identifiers that are currently marked for restart.
		// This list is passed to FindMatchingIdentifiers for exact/replica/service matching.
		restartingNames := make([]string, 0, len(restartByIdentifier))
		for name, restarting := range restartByIdentifier {
			if restarting {
				restartingNames = append(restartingNames, name)
			}
		}

		matches := sorter.FindMatchingIdentifiers(link, restartingNames)

		if len(matches) > 0 {
			dependentProject := getProject(log, dependentContainer)

			// Prefer any candidate that shares the dependent's project (from labels).
			for _, matchedID := range matches {
				matchedContainer, ok := idToContainer[matchedID]
				if !ok || matchedContainer == nil {
					continue
				}

				if getProject(log, matchedContainer) == dependentProject && dependentProject != "" {
					log.Debug().
						Str("link", link).
						Str("matched", matchedID).
						Str("reason", "same-project via FindMatchingIdentifiers").
						Msg("Found restarting linked container")

					return matchedID
				}
			}

			// Determine if any match from FindMatchingIdentifiers was an
			// exact or replica match (vs pure service-only). We are more
			// permissive with exact/replica results.
			hasExactOrReplica := false

			for _, matchID := range matches {
				if matchID == link {
					hasExactOrReplica = true

					break
				}

				if strings.HasPrefix(matchID, link+"-") {
					suffix := matchID[len(link)+1:]
					if sorter.IsPositiveInteger(suffix) {
						hasExactOrReplica = true

						break
					}
				}
			}

			// Only treat the link as an explicit project-qualified reference (and
			// therefore block a pure service-only result) when it starts with a
			// known project prefix. Bare service names (even hyphenated ones such
			// as "watchtower-test-database") are permitted to resolve via the
			// service fallback.
			if !isExplicitProjectRef || restartByIdentifier[link] || hasExactOrReplica {
				chosen := matches[0]
				log.Debug().
					Str("link", link).
					Str("matched", chosen).
					Str("reason", "first match via FindMatchingIdentifiers").
					Msg("Found restarting linked container")

				return chosen
			}

			log.Debug().
				Str("link", link).
				Msg("Qualified link did not match any restarting candidate")
		}

		// Skip the bare service-name fallback for links that are explicit
		// project-qualified references.
		if isExplicitProjectRef {
			continue
		}

		dependentProject := getProject(log, dependentContainer)
		linkService := sorter.ExtractServiceName(link)

		// Build the list of currently restarting containers that match on service name alone.
		var serviceCandidates []string

		for _, name := range restartingNames {
			if sorter.ExtractServiceName(name) == linkService {
				serviceCandidates = append(serviceCandidates, name)
			}
		}

		if len(serviceCandidates) > 0 {
			// Prefer a candidate from the same project as the dependent.
			for _, cand := range serviceCandidates {
				c, ok := idToContainer[cand]
				if !ok || c == nil {
					continue
				}

				if getProject(log, c) == dependentProject &&
					dependentProject != "" {
					log.Debug().
						Str("link", link).
						Str("matched", cand).
						Str("reason", "same-project service fallback").
						Msg("Found restarting linked container")

					return cand
				}
			}

			// No same-project service match found. Take the first candidate
			// after sorting for deterministic behavior.
			sort.Strings(serviceCandidates)
			chosen := serviceCandidates[0]
			log.Debug().
				Str("link", link).
				Str("matched", chosen).
				Str("reason", "first service fallback").
				Msg("Found restarting linked container")

			return chosen
		}
	}

	log.Debug().Msg("No restarting linked container found")

	return ""
}

// getProject extracts the project name from a container's compose project label.
//
// Falls back to parsing the leading segment of the container name if no project
// label is present.
//
// Parameters:
//   - c: Container to inspect.
//
// Returns:
//   - string: Project name, or "" if none can be determined.
func getProject(log *zerolog.Logger, c types.Container) string {
	monitoredContainer, ok := c.(*container.Container)
	if ok {
		info := monitoredContainer.ContainerInfo()
		if info != nil && info.Config != nil {
			project := compose.GetProjectName(log, info.Config.Labels)
			if project != "" {
				return project
			}
		}
	}
	// Fallback to parsing from container name
	containerName := c.Name()

	idx := strings.Index(containerName, "-")
	if idx > 0 {
		return containerName[:idx]
	}

	return ""
}

// parseReference validates a Docker image reference with logging.
//
// Parameters:
//   - imageName: Image name to parse.
//   - configImage: Config.Image value for log context.
//   - fallbackImage: Fallback image name for log context.
//   - cont: Container being processed (for log fields).
//
// Returns:
//   - error: Non-nil if the image name cannot be parsed as a Docker reference.
func parseReference(log *zerolog.Logger, imageName, configImage, fallbackImage string,
	cont types.Container,
) error {
	// Set up logging with container and image details.
	clogVal := log.With().
		Str("container", cont.Name()).
		Str("image", imageName).
		Logger()
	clog := &clogVal

	// Parse the image reference using the Docker reference library.
	normalizedRef, err := reference.ParseDockerRef(imageName)
	if err != nil {
		clog.Debug().
			Err(err).
			Str("image_name", imageName).
			Msg("Failed to parse image reference")

		return fmt.Errorf(
			"failed to parse image reference %s: %w",
			imageName,
			err,
		)
	}

	// Log successful parsing with reference type and context.
	clog.Debug().
		Str("image_name", imageName).
		Str("config_image", configImage).
		Str("fallback_image", fallbackImage).
		Str("ref_type", fmt.Sprintf("%T", normalizedRef)).
		Msg("Parsed image reference")

	return nil
}

// isPinned checks if a container's image is pinned by a digest reference.
//
// It resolves a usable image name from ImageName(), Config.Image, or a fallback,
// then delegates pin detection to container.IsImagePinnedByDigest. Pin detection
// runs before parse-fallback so a digest reference is not replaced by a non-pinned
// fallback when ParseDockerRef fails. If pinned, it marks the container as scanned.
//
// Parameters:
//   - cont: The container to check for a pinned image.
//   - progress: The progress tracker to update for scanned or skipped containers.
//   - params: Update parameters for monitor-only check.
//
// Returns:
//   - bool: True if the image is pinned by digest, false otherwise.
//   - error: Non-nil if no valid image reference can be resolved, nil on success.
func isPinned(log *zerolog.Logger, cont types.Container,
	progress *session.Progress,
	config types.UpdateParams,
) (bool, error) {
	// Set up logging with container and image details for debugging.
	clogVal := log.With().
		Str("container", cont.Name()).
		Str("image", cont.ImageName()).
		Logger()
	clog := &clogVal

	// Get initial image name and configuration.
	imageName := cont.ImageName()

	var configImage string
	if info := cont.ContainerInfo(); info != nil && info.Config != nil {
		configImage = info.Config.Image
	}

	fallbackImage := getFallbackImage(cont)

	// Check if ImageName is invalid and fall back to Config.Image or a derived name.
	if isInvalidImageName(imageName) {
		clog.Debug().
			Str("invalid_image", imageName).
			Msg("Invalid ImageName detected")

		if configImage != "" && !isInvalidImageName(configImage) {
			imageName = configImage
			clog.Debug().
				Str("config_image", configImage).
				Msg("Using Config.Image as fallback")
		} else {
			imageName = fallbackImage
			clog.Debug().
				Str("fallback_image", fallbackImage).
				Msg("Using derived fallback image")
		}
	}

	// If the final imageName is still invalid, skip the container.
	if isInvalidImageName(imageName) {
		return false, errInvalidImageReference
	}

	// Detect digests before parse-fallback. A repo@sha256 reference must stay
	// pinned even when ParseDockerRef fails for unrelated reasons.
	if container.IsImagePinnedByDigest(imageName) {
		clog.Debug().
			Bool("is_digested", true).
			Msg("Pinned image detected, marking as scanned")
		progress.AddScanned(log,
			cont,
			cont.ImageID(),
			config,
		)

		return true, nil
	}

	// Non-pinned names must still be parseable. Retry with fallback when needed.
	err := parseReference(log, imageName, configImage, fallbackImage, cont)
	if err != nil {
		if imageName != fallbackImage {
			clog.Debug().Msg("Retrying with fallback image")

			fallbackErr := parseReference(log,
				fallbackImage,
				configImage,
				fallbackImage,
				cont,
			)
			if fallbackErr != nil {
				return false, err
			}

			// Fallback name might itself be digest-pinned (unlikely but consistent).
			if container.IsImagePinnedByDigest(fallbackImage) {
				clog.Debug().
					Bool("is_digested", true).
					Msg("Pinned image detected via fallback, marking as scanned")
				progress.AddScanned(log,
					cont,
					cont.ImageID(),
					config,
				)

				return true, nil
			}

			return false, nil
		}

		return false, err
	}

	return false, nil
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
func performRollingRestart(log *zerolog.Logger, ctx context.Context,
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

	log.Debug().
		Strs("processing_order", containerNames).
		Msg("Starting performRollingRestart")

	// Process containers in forward order to respect dependency chains.
	for i := range containers {
		// Check for context cancellation to enable prompt exit when context is canceled.
		select {
		case <-ctx.Done():
			// Handle the current container that was not processed due to cancellation.
			c := containers[i]
			log.Info().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("container_id", c.ID().ShortID()).
				Msg("Skipped container restart due to context cancellation")
			failed[c.ID()] = fmt.Errorf("restart skipped: %w", ctx.Err())

			// Handle remaining containers that were not processed due to cancellation.
			for j := i + 1; j < len(containers); j++ {
				skipped := containers[j]
				log.Info().
					Str("container", skipped.Name()).
					Str("image", skipped.ImageName()).
					Str("container_id", skipped.ID().ShortID()).
					Msg("Skipped container restart due to context cancellation")
				failed[skipped.ID()] = fmt.Errorf("restart skipped: %w", ctx.Err())
			}

			return failed, fmt.Errorf("rolling restart canceled: %w", ctx.Err())
		default:
		}

		c := containers[i]
		if !c.ToRestart() {
			continue
		}

		fields := map[string]any{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		log.Debug().
			Fields(fields).
			Msg("Processing container for rolling restart")

		// Mark for update if stale
		if c.IsStale() && progress != nil {
			progress.MarkForUpdate(log, c.ID())
		}

		// Stop the container, handling any errors.
		err := stopStaleContainer(log, ctx, c, client, config)
		if err != nil {
			failed[c.ID()] = err
		} else {
			newContainerID, renamed, err := restartStaleContainer(log,
				ctx,
				c,
				client,
				config,
			)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Set the new container ID in progress
				if progress != nil {
					status, exists := (*progress)[c.ID()]
					if exists {
						status.SetNewContainerID(newContainerID)
						// Mark as restarted if not stale (not updated)
						if !c.IsStale() {
							progress.MarkRestarted(log, c.ID())
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
					log.Warn().
						Err(waitErr).
						Fields(fields).
						Msg("Failed to wait for container to become healthy")

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

					log.Debug().
						Fields(fields).
						Msg("Updated container")
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
func stopContainersInReversedOrder(log *zerolog.Logger, ctx context.Context,
	containers []types.Container,
	client container.Client,
	config types.UpdateParams,
) (map[types.ContainerID]error, []types.RemovedImageInfo) {
	failed := make(map[types.ContainerID]error, len(containers))
	stopped := make([]types.RemovedImageInfo, 0, len(containers))

	// Stop containers in reverse order to avoid breaking dependencies.
	for i, v := range slices.Backward(containers) {
		c := v

		// Check for context cancellation to avoid additional work when context is canceled.
		// First, log and track the current container, then iterate remaining containers.
		if ctx.Err() != nil {
			// Handle the current container that was not processed due to cancellation.
			log.Info().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("container_id", c.ID().ShortID()).
				Msg("Skipped container stop due to context cancellation")
			failed[c.ID()] = fmt.Errorf("stop skipped: %w", ctx.Err())

			// Handle remaining containers that were not processed due to cancellation.
			for j := i - 1; j >= 0; j-- {
				skipped := containers[j]
				log.Info().
					Str("container", skipped.Name()).
					Str("image", skipped.ImageName()).
					Str("container_id", skipped.ID().ShortID()).
					Msg("Skipped container stop due to context cancellation")
				failed[skipped.ID()] = fmt.Errorf("stop skipped: %w", ctx.Err())
			}

			return failed, stopped
		}

		fields := map[string]any{
			"container": c.Name(),
			"image":     c.ImageName(),
		}

		err := stopStaleContainer(log, ctx, c, client, config)
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

			log.Debug().
				Fields(fields).
				Msg("Stopped container")
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
func stopStaleContainer(log *zerolog.Logger, ctx context.Context,
	container types.Container,
	client container.Client,
	config types.UpdateParams,
) error {
	fields := map[string]any{
		"container": container.Name(),
		"image":     container.ImageName(),
	}

	// Skip Watchtower containers to avoid self-interruption.
	if container.IsWatchtower() {
		log.Debug().
			Fields(fields).
			Msg("Skipping Watchtower container")

		return nil
	}

	// Skip containers not marked for restart (e.g., not stale or linked).
	if !container.ToRestart() {
		return nil
	}

	log.Debug().
		Fields(fields).
		Msg("Stopping container for restart")

	// Verify configuration for linked containers to ensure restart compatibility.
	if container.IsLinkedToRestarting() {
		err := container.VerifyConfiguration()
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to verify container configuration")

			return fmt.Errorf("%w: %w", errVerifyConfigFailed, err)
		}
	}

	// Execute pre-update lifecycle hooks if enabled, checking for skip conditions.
	if config.LifecycleHooks {
		skipUpdate, err := lifecycle.ExecutePreUpdateCommand(log,
			ctx,
			client,
			container,
			config.LifecycleUID,
			config.LifecycleGID,
		)
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Pre-update command execution failed")

			return fmt.Errorf("%w: %w", errPreUpdateFailed, err)
		}

		if skipUpdate {
			log.Debug().
				Fields(fields).
				Msg("Skipping container due to pre-update exit code 75")

			return errSkipUpdate
		}
	}

	// Stop the container with the configured timeout.
	err := client.StopAndRemoveContainer(
		ctx,
		container,
		config.Timeout,
	)
	if err != nil {
		// Check if the container is already gone (e.g., "No such container" error).
		// Treat this as non-fatal, similar to RemoveExcessWatchtowerInstances.
		if cerrdefs.IsNotFound(err) {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Container not found, treating as already stopped")

			return nil
		}

		log.Error().
			Err(err).
			Fields(fields).
			Msg("Failed to stop container")

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
func restartContainersInSortedOrder(log *zerolog.Logger, ctx context.Context,
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
	for i := range containers {
		c := containers[i]

		if !c.ToRestart() {
			continue
		}

		fields := map[string]any{
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

		// Parent cancellation must not strand containers already removed in the
		// stop phase. restartStaleContainer uses a detached context for create/start,
		// so continue recreating stopped (and self-update) instances. Containers that
		// were never stopped can be skipped safely.
		if ctx.Err() != nil && !c.IsWatchtower() && !wasStopped {
			log.Info().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("container_id", c.ID().ShortID()).
				Msg("Skipped container restart due to context cancellation")
			failed[c.ID()] = fmt.Errorf("restart skipped: %w", ctx.Err())

			continue
		}

		if ctx.Err() != nil && (c.IsWatchtower() || wasStopped) {
			log.Info().
				Fields(fields).
				Str("container_id", c.ID().ShortID()).
				Msg("Parent context canceled. Continuing restart of stopped container")
		}

		// Restart Watchtower containers regardless of stoppedImages, as they are renamed.
		// Otherwise, restart only containers that were previously stopped.
		if c.IsWatchtower() || wasStopped {
			newContainerID, renamed, err := restartStaleContainer(log,
				ctx,
				c,
				client,
				config,
			)
			if err != nil {
				failed[c.ID()] = err
			} else {
				// Set the new container ID in progress
				if progress != nil {
					status, exists := (*progress)[c.ID()]
					if exists {
						status.SetNewContainerID(newContainerID)
						// Mark as restarted if not stale (not updated)
						if !c.IsStale() {
							progress.MarkRestarted(log, c.ID())
						}
					}
				}

				log.Debug().
					Fields(fields).
					Msg("Restarted container")

				if renamed {
					renamedContainers[c.ID()] = true
				}
				// Only collect cleaned image info for stale containers that were
				// not renamed, as renamed containers (Watchtower self-updates)
				// are cleaned up by CheckForMultipleWatchtowerInstances
				// in the new container.
				if c.IsStale() && !renamedContainers[c.ID()] {
					addCleanupImageInfo(
						cleanupImageInfos,
						c.ImageID(),
						c.ImageName(),
						c.Name(),
						c.ID(),
					)
				}
			}
		}
	}

	return failed
}

// addCleanupImageInfo adds cleanup info if not already present.
//
// Deduplication is based on the (ImageID, ContainerName) pair so that when
// notifications are split by container, each container that used the old image
// receives its own "Removing image" entry.
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
		if existing.ImageID == imageID && existing.ContainerName == containerName {
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
func restartStaleContainer(log *zerolog.Logger, ctx context.Context,
	sourceContainer types.Container,
	client container.Client,
	config types.UpdateParams,
) (types.ContainerID, bool, error) {
	// Create a detached context to survive parent context cancellation.
	// This ensures container cleanup and update operations complete even if the
	// parent context is canceled during the restart process.
	// A finite fallback is applied when config.Timeout is non-positive, and the
	// create/start phase uses at least defaultCreateStartTimeout to accommodate
	// slow hosts.
	detachedTimeout := max(restartPolicyTimeout(config.Timeout), defaultCreateStartTimeout)

	detachedCtx, cancelDetached := context.WithTimeout(
		context.Background(),
		detachedTimeout,
	)

	defer cancelDetached()

	fields := map[string]any{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	}

	var (
		renamed                bool
		originalWatchtowerName string
	)

	// Rename Watchtower containers regardless of NoRestart flag,
	// but skip in run-once mode as there's no need to avoid conflicts
	// with a continuously running instance.
	if sourceContainer.IsWatchtower() && !config.RunOnce {
		// Opt-in ephemeral self-update: use a short-lived orchestrator container
		// to perform the transition atomically. The orchestrator handles stopping
		// the old container, creating and starting the new one, and cleanup.
		// EphemeralSelfUpdate returns immediately after starting the orchestrator.
		// The orchestrator completes the replacement asynchronously. The current
		// Watchtower process will be stopped by the orchestrator shortly after.
		if config.EphemeralSelfUpdate {
			log.Debug().
				Fields(fields).
				Msg("Using ephemeral self-update")

			_, renamed, err := EphemeralSelfUpdate(log,
				ctx,
				client,
				sourceContainer,
				config,
			)
			if err != nil {
				return "", false, err
			}

			// Skip health check and post-update hooks: the new container's ID is
			// not known to this process, and the orchestrator will stop this process
			// shortly. The new Watchtower instance handles its own lifecycle.
			return "", renamed, nil
		}

		targetOldName := types.WatchtowerOldPrefix + sourceContainer.ID().ShortID()

		// Redundant rename guard: the lingering old instance already has the
		// target name from a prior rename. Skip to avoid a same-name error.
		if container.IsOldContainer(sourceContainer.Name()) {
			log.Debug().
				Fields(fields).
				Str("target_name", targetOldName).
				Msg("Skipping rename of already-renamed Watchtower container")

			renamed = true
		} else {
			originalWatchtowerName = sourceContainer.Name()
			newName := targetOldName

			err := client.RenameContainer(
				ctx,
				sourceContainer,
				newName,
			)
			if err != nil {
				log.Debug().
					Err(err).
					Str("container", sourceContainer.Name()).
					Str("new_name", newName).
					Msg("Failed to rename Watchtower container")

				return "",
					false,
					fmt.Errorf(
						"%w: %w",
						errRenameWatchtowerFailed,
						err,
					)
			}

			log.Debug().
				Fields(fields).
				Str("new_name", newName).
				Msg("Renamed Watchtower container")

			renamed = true
		}
	}

	// For Watchtower self-updates, accumulate container ID chain in labels.
	if sourceContainer.IsWatchtower() {
		c, ok := sourceContainer.(*container.Container)
		if ok {
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
				log.Debug().
					Fields(fields).
					Str("container_chain", newChain).
					Msg("Updated container chain label for Watchtower self-update")
			}
		}
	}

	// Create the new container with updated configuration.
	//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
	newContainerID, err := client.CreateContainer(detachedCtx, sourceContainer)
	if err != nil {
		log.Debug().
			Err(err).
			Fields(fields).
			Msg("Failed to create container")

		// Restore the original name so the running instance is not left only as
		// watchtower-old-* with the canonical name free and unused.
		// Use a fresh context here: detachedCtx may already be expired if the
		// create call hit the timeout, and we do not want recovery to fail for
		// the same reason.
		if renamed && originalWatchtowerName != "" && sourceContainer.IsWatchtower() {
			renameBackCtx, renameBackCancel := context.WithTimeout(
				context.Background(),
				restartPolicyTimeout(config.Timeout),
			)
			defer renameBackCancel()

			//nolint:contextcheck // fresh context is intentional for recovery after timeout
			renameBackErr := client.RenameContainer(
				renameBackCtx,
				sourceContainer,
				originalWatchtowerName,
			)
			if renameBackErr != nil {
				log.Debug().
					Err(renameBackErr).
					Fields(fields).
					Str("original_name", originalWatchtowerName).
					Msg("Failed to rename Watchtower container back after create failure")
			} else {
				renamed = false

				log.Debug().
					Fields(fields).
					Str("original_name", originalWatchtowerName).
					Msg("Restored Watchtower container name after create failure")
			}
		}

		return "",
			renamed,
			fmt.Errorf(
				"%w: %w",
				errCreateContainerFailed,
				err,
			)
	}

	// Start the new container based on restart settings:
	//   - Watchtower containers bypass the NoRestart check
	//   - All containers (including Watchtower) start only if they were running or ReviveStopped is enabled
	if (!config.NoRestart || sourceContainer.IsWatchtower()) && (sourceContainer.IsRunning() || config.ReviveStopped) {
		log.Debug().
			Fields(fields).
			Msg("Starting container with updated configuration")

		//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
		err = client.StartContainerByID(detachedCtx, newContainerID)
		if err != nil {
			log.Debug().
				Err(err).
				Fields(fields).
				Msg("Failed to start container")

			// On start failure after a Watchtower rename, remove the failed replacement
			// container only. The renamed source still holds the running process and must
			// remain so the host keeps a Watchtower instance.
			// Use a fresh context because detachedCtx may already be expired if the start
			// call hit the timeout and we do not want cleanup to fail for the same reason.
			if renamed && sourceContainer.IsWatchtower() {
				log.Debug().
					Fields(fields).
					Str("new_id", newContainerID.ShortID()).
					Msg("Cleaning up failed Watchtower container")

				cleanupCtx, cleanupCancel := context.WithTimeout(
					context.Background(),
					restartPolicyTimeout(config.Timeout),
				)
				defer cleanupCancel()

				//nolint:contextcheck // fresh context is intentional for cleanup after timeout
				failedNew, getErr := client.GetContainer(cleanupCtx, newContainerID)
				if getErr != nil {
					log.Debug().
						Err(getErr).
						Fields(fields).
						Str("new_id", newContainerID.ShortID()).
						Msg("Failed to inspect failed Watchtower container for cleanup")
				} else {
					//nolint:contextcheck // fresh context is intentional for cleanup after timeout
					cleanupErr := client.StopAndRemoveContainer(
						cleanupCtx,
						failedNew,
						config.Timeout,
					)
					if cleanupErr != nil {
						log.Debug().
							Err(cleanupErr).
							Fields(fields).
							Str("new_id", newContainerID.ShortID()).
							Msg("Failed to stop failed Watchtower container")
					}
				}
			}

			return "",
				renamed,
				fmt.Errorf(
					"%w: %w",
					errStartContainerFailed,
					err,
				)
		}

		log.Info().
			Fields(fields).
			Str("new_id", newContainerID.ShortID()).
			Msg("Started new container")

		// Run post-update lifecycle hooks for restarting containers if enabled.
		if sourceContainer.ToRestart() && config.LifecycleHooks {
			log.Debug().
				Fields(fields).
				Msg("Executing post-update command")
			//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
			lifecycle.ExecutePostUpdateCommand(log,
				detachedCtx,
				client,
				newContainerID,
				config.LifecycleUID,
				config.LifecycleGID,
			)
		}
	}

	// For renamed Watchtower containers, update restart policy to "no" to prevent auto-restart.
	if renamed && sourceContainer.IsWatchtower() {
		log.Debug().
			Fields(fields).
			Msg("Updating restart policy for old Watchtower container")

		//nolint:contextcheck // Using detached context intentionally to survive parent cancellation
		client.SetNoRestartPolicy(detachedCtx, sourceContainer)
	}

	return newContainerID, renamed, nil
}

// deriveScopeFromCurrentContainer finds the current container by ID in the
// provided list and returns its scope and whether it was found. Returns ""
// for unscoped containers (found but no scope) and found=false when the
// container ID isn't in the list. The caller normalizes "" to "none" for
// cleanup and skips cleanup when found=false.
func deriveScopeFromCurrentContainer(log *zerolog.Logger, allContainers []types.Container,
	currentContainerID types.ContainerID,
) (string, bool) {
	for _, c := range allContainers {
		if c.ID() == currentContainerID {
			containerScope, containerHasScope := c.Scope()
			if !containerHasScope || containerScope == "" {
				return "", true
			}

			return containerScope, true
		}
	}

	log.Debug().
		Str("current_container_id", string(currentContainerID)).
		Msg("Current container not found in list")

	return "", false
}

// restartPolicyTimeout returns the timeout to use for restart policy operations.
// If the provided timeout is positive, it is returned as-is.
// Otherwise, a finite fallback is used to prevent indefinite blocking.
func restartPolicyTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}

	return defaultRestartPolicyTimeout
}
