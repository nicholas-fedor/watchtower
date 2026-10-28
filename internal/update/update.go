package update

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/container/hooks"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	"github.com/nicholas-fedor/watchtower/internal/update/changelog"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	"github.com/nicholas-fedor/watchtower/internal/update/rebuild"
	"github.com/nicholas-fedor/watchtower/internal/update/selfupdate"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
	"github.com/nicholas-fedor/watchtower/internal/update/staleness"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for update operations.
var (
	// errSortDependenciesFailed indicates a failure to sort containers by dependencies.
	errSortDependenciesFailed = errors.New("failed to sort containers by dependencies")
	// errParseImageReference indicates a failure to parse a container's image reference.
	errParseImageReference = errors.New("failed to parse image reference")
	// errCircularDependency indicates a circular dependency between containers.
	errCircularDependency = errors.New("circular dependency detected")
	// errSelfDependency indicates a container has a self-dependency.
	errSelfDependency = errors.New("container has self-dependency")
	// errOldSelfDetected indicates the current container is an old
	// Watchtower container that should not be running.
	errOldSelfDetected = errors.New("current container is an old Watchtower container")
)

// reportedProblems records the dependency problems already reported as
// warnings, so a problem that persists across scans notifies only once.
type reportedProblems struct {
	// mu guards seen.
	mu sync.Mutex
	// seen holds a key for each reported problem.
	seen map[string]struct{}
}

const (
	// defaultPullFailureDelay defines the default delay after a failed Watchtower self-update pull in run-once mode.
	defaultPullFailureDelay = 5 * time.Minute

	// maxReportedProblems bounds the dependency problems recorded during a
	// process, since the containers they name can be removed at any time.
	maxReportedProblems = 1024
)

// dependencyProblems holds the dependency problems reported during this
// process. Keys include the container ID, so a recreated container, or one
// whose labels changed, is reported again.
var dependencyProblems = &reportedProblems{seen: make(map[string]struct{})}

// update scans and updates containers based on parameters.
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
//   - rateLimiter: Rate limiter shared by registry requests. Must not be nil.
//   - gitClient: Git monitor client. Nil when Git monitoring is unused.
//
// Returns:
//   - types.Report: Session report summarizing scanned, updated, and failed containers.
//   - []types.RemovedImageInfo: Slice of cleaned image info to clean up after updates.
//   - error: Non-nil if listing or sorting fails, nil on success.
func update(
	log *zerolog.Logger,
	ctx context.Context,
	client containerClient.Client,
	config types.UpdateParams,
	rateLimiter *ratelimit.Limiter,
	gitClient ...*git.Client,
) (types.Report, []types.RemovedImageInfo, error) {
	// Check for context cancellation early
	select {
	case <-ctx.Done():
		return nil, nil, fmt.Errorf("update canceled: %w", ctx.Err())
	default:
	}

	var watcher *git.Client
	if len(gitClient) > 0 {
		watcher = gitClient[0]
	}

	gitSession := rebuild.NewSession(watcher)

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
					recoveredContainer, recovered := selfupdate.TryRecoverOrphanedContainer(log,
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
					disableOldWatchtowerRestarts(log, setNoRestartCtx, c, client)

					return nil, nil, errOldSelfDetected
				}

				break
			}
		}
	}

	// Clean up any old Watchtower containers that linger from a previous
	// self-update. This runs each update cycle to catch containers that the
	// startup cleanup may have missed (e.g., they were still stopping at startup).
	// It stays within the current container's scope.
	_, cleanupErr := cleanup.RemoveOldWatchtowerContainers(log,
		ctx,
		client,
		config.Cleanup,
		config.CurrentContainerID,
		&cleanupImageInfos,
	)
	if cleanupErr != nil {
		log.Warn().
			Err(cleanupErr).
			Msg("Failed to clean up old Watchtower containers, continuing update cycle")
	}

	// Create a progress tracker for reporting scanned, updated, and skipped containers.
	progress := &state.Progress{}
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
		hooks.ExecutePreChecks(log, ctx, client, config, filteredContainers)
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
		if hasSelfDependency(monitoredContainer) {
			progress.AddSkipped(log,
				monitoredContainer,
				errSelfDependency,
				config,
			)
			log.WithLevel(dependencyProblems.level("self-dependency", string(monitoredContainer.ID()))).
				Str("container", monitoredContainer.Name()).
				Str("id", monitoredContainer.ID().ShortID()).
				Msg("Skipping container update (self-dependency)")
		}
	}

	// Build the dependency graph once. The cycle check, every dependency sort,
	// and the implicit restarts below use it, so each link resolves to the same
	// container throughout the scan. A graph that cannot be built fails the
	// first sort. Unmonitored containers are passed so that a link naming one
	// resolves to it, and is left alone, rather than to a monitored container.
	graph, graphErr := sorter.NewDependencyGraph(log,
		filteredContainers,
		unmonitoredContainers(allContainers, filteredContainers),
		config.UseComposeDependsOn,
	)
	if graphErr == nil {
		for _, link := range graph.UnresolvedDependsOn() {
			log.WithLevel(dependencyProblems.level("unresolved depends-on", string(link.Container.ID()), link.Link)).
				Str("container", link.Container.Name()).
				Str("depends_on", link.Link).
				Msg("Ignoring depends-on entry that names no container")
		}
	}

	// Skip every container in a circular dependency. Cycle members are also
	// kept out of the dependency sorts and implicit restarts below, so a cycle
	// cannot fail the session or be restarted through a link to an updated
	// container.
	inCycle := make(map[types.ContainerID]struct{})

	var cycleMembers []types.Container
	if graphErr == nil {
		cycleMembers = graph.CycleMembers()
	}

	for _, c := range cycleMembers {
		inCycle[c.ID()] = struct{}{}

		progress.AddSkipped(log, c, errCircularDependency, config)
		log.WithLevel(dependencyProblems.level("circular dependency", string(c.ID()))).
			Str("container", c.Name()).
			Str("id", c.ID().ShortID()).
			Msg("Skipping container update (circular dependency)")
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
		isPinnedVal, err := staleness.IsPinned(log, sourceContainer, progress, config, gitSession)
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

	// One resolver per session so sibling containers on the same upstream image
	// share a single release tag lookup.
	tagResolver := changelog.NewResolver(changelog.NewDigestFetcher(config.Registry, rateLimiter))

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
				newDigest   string
				gitWatched  bool
				checkErr    error
				verifyErr   error
			)

			// Determine if the container is stale and needs updating.
			// If the container is Watchtower and SkipSelfUpdate is enabled, skip the update
			// by setting stale to false and using the current image. Otherwise, check staleness.
			switch {
			case sourceContainer.IsWatchtower() && config.SkipSelfUpdate:
				stale = false
				newestImage = sourceContainer.ImageID()
			case gitSession.Watching(log, sourceContainer, config):
				gitWatched = true

				stale, newestImage, checkErr = gitSession.Check(log, ctx, sourceContainer, config)
			default:
				stale, newestImage, newDigest, checkErr = client.IsContainerStale(
					ctx,
					sourceContainer,
					config,
				)
			}

			if checkErr != nil && (errors.Is(checkErr, context.Canceled) || errors.Is(checkErr, context.DeadlineExceeded)) {
				return fmt.Errorf("staleness check canceled: %w", checkErr)
			}

			// Determine if the container should be updated based on staleness and config.
			shouldUpdate := staleness.ShouldUpdate(sourceContainer, stale, config)
			if gitSession.Watching(log, sourceContainer, config) && sourceContainer.IsNoPull(config) {
				shouldUpdate = false
			}

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

			// Record the new image's OCI metadata so a notification can describe
			// what the container is moving to. This runs only for a container
			// that will actually be updated. The inspect and the optional
			// release tag lookup happen before resultMu is taken, because a
			// registry round trip under that lock would serialize every
			// parallel check behind one container's request.
			// A Git-watched container is excluded because the build path resolves
			// and logs its own changelog from the resolved tag afterwards.
			resolveLatest := stale && shouldUpdate && !gitWatched && checkErr == nil && verifyErr == nil

			var (
				changelogVars provenance.ChangelogVars
				latestAnns    oci.Annotations
			)

			if resolveLatest {
				latest := staleness.LatestImage{
					Annotations: client.GetImageAnnotations(ctx, sourceContainer.ImageName()),
					Digest:      newDigest,
				}

				changelogVars, latestAnns = staleness.ResolveLatestImageMeta(
					log,
					ctx,
					tagResolver,
					sourceContainer,
					config,
					latest,
				)
			}

			resultMu.Lock()
			defer resultMu.Unlock()

			// Handle staleness check results, logging skips or adding to the progress report.
			switch {
			case checkErr != nil:
				// Skip containers with staleness check errors, marking them as skipped.
				if !errors.Is(checkErr, containerImage.ErrImageCooldown) {
					parallelStaleCheckFailed++
				}

				if ratelimit.Is(checkErr) {
					// One line per container per cycle. Log-based notifications
					// carry it, and the report template lists the container under
					// Failed instead. A Retry-After beyond the honor window was
					// already logged at warn by the retry loop.
					if !ratelimit.ExceedsHonorWindow(checkErr) {
						clog.Warn().
							Err(checkErr).
							Msg("Registry rate limit retries exhausted. Container failed for this cycle")
					}

					progress.AddFailed(log, sourceContainer, checkErr, config)
				} else {
					progress.AddSkipped(log, sourceContainer, checkErr, config)
				}

				// Record the cooldown details for reports and notifications. The
				// *container.CooldownError carries them.
				cooldownErr, ok := errors.AsType[*containerImage.CooldownError](checkErr)
				if ok {
					progress.SetCooldownInfo(log,
						sourceContainer.ID(),
						cooldownErr.Age,
						cooldownErr.Delay,
						cooldownErr.Remaining,
						cooldownErr.EligibleAt,
					)
				} else if errors.Is(checkErr, containerImage.ErrImageCooldown) {
					// Fallback for plain sentinel (keeps basic deferral visible)
					progress.SetCooldownInfo(log, sourceContainer.ID(), "", "", "", time.Time{})
				}

				// Track if Watchtower self-update pull failed for safeguard.
				// Only set to true if we actually attempted a self-update
				// (i.e., SkipSelfUpdate is false) and the error is a real
				// failure, not a cooldown deferral.
				if sourceContainer.IsWatchtower() &&
					!config.SkipSelfUpdate &&
					!errors.Is(checkErr, containerImage.ErrImageCooldown) {
					parallelWatchtowerPullFailed = true
				}
			case verifyErr != nil:
				parallelStaleCheckFailed++

				progress.AddSkipped(log, sourceContainer, verifyErr, config)

				if sourceContainer.IsWatchtower() &&
					!config.SkipSelfUpdate &&
					!errors.Is(verifyErr, containerImage.ErrImageCooldown) {
					parallelWatchtowerPullFailed = true
				}
			default:
				// For fresh containers, set newestImage to current image ID for proper categorization.
				// (The cooldown decision happens in the image client's PullImage, after the
				// digest staleness check and before any layer is pulled.)
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

			// Write the pre-resolved metadata now that the session state is locked.
			if resolveLatest {
				staleness.ApplyLatestImageMeta(
					clog,
					progress,
					sourceContainer,
					config,
					changelogVars,
					latestAnns,
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
	// Cycle members cannot be ordered, so they are placed after the others.
	// Every sort keeps only the graph's dependencies between the containers it
	// sorts, so leaving a container out cannot redirect a link to a different
	// container and form a cycle that the graph does not have.
	sortable := make([]types.Container, 0, len(filteredContainers))
	unsortable := make([]types.Container, 0, len(inCycle))

	for _, c := range filteredContainers {
		if _, skip := inCycle[c.ID()]; skip {
			unsortable = append(unsortable, c)
		} else {
			sortable = append(sortable, c)
		}
	}

	err = graphErr
	if err == nil {
		err = graph.Sort(log, sortable)
	}

	if err != nil {
		// Log and return an error if dependency sorting fails.
		log.Debug().
			Err(err).
			Msg("Failed to sort containers by dependencies")

		return nil, cleanupImageInfos, fmt.Errorf(
			"%w: %w",
			errSortDependenciesFailed,
			err,
		)
	}

	// Write the order back, with cycle members last.
	copy(filteredContainers, sortable)
	copy(filteredContainers[len(sortable):], unsortable)

	// Collect all containers to restart: updates, and containers linked to
	// restarting ones, which restart without updating.
	allContainersToRestart := reconcileImplicitRestartsExcluding(log,
		graph,
		allContainers,
		filteredContainers,
		config,
		inCycle,
	)

	// Sort containers to restart by dependencies to ensure correct update and restart order.
	err = graph.Sort(log, allContainersToRestart)
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Failed to sort all containers to restart by dependencies")

		return nil, cleanupImageInfos, fmt.Errorf(
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

	gitFailed := make(map[types.ContainerID]error)
	gitSession.PrepareRebuilds(
		log,
		ctx,
		client,
		allContainersToRestart,
		config,
		progress,
		gitFailed,
	)
	progress.UpdateFailed(log, gitFailed)

	// A failed Git apply cleared Stale. Recompute linked restarts so those
	// dependencies are not stopped. Containers that are still stale stay anchors.
	// Cycle members stay excluded, as in the first pass.
	excluded := make(map[types.ContainerID]struct{}, len(inCycle))
	maps.Copy(excluded, gitSession.NoRestartBuiltIDs(config))
	maps.Copy(excluded, inCycle)

	allContainersToRestart = reconcileImplicitRestartsExcluding(
		log,
		graph,
		allContainers,
		filteredContainers,
		config,
		excluded,
	)

	err = graph.Sort(log, allContainersToRestart)
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Failed to sort all containers to restart by dependencies")

		return nil, cleanupImageInfos, fmt.Errorf(
			"%w: %w",
			errSortDependenciesFailed,
			err,
		)
	}

	// Compose apply already recreated those services. Do not inspect-recreate.
	allContainersToRestart = gitSession.ExcludeApplied(allContainersToRestart)

	// Git no-restart builds must not enter stop/create.
	// The default non-rolling path stops first.
	// Skipping their recreation only after the stop would leave the running
	// container deleted.
	allContainersToRestart = gitSession.ExcludeNoRestart(
		allContainersToRestart,
		config,
	)

	if config.RollingRestart {
		// Apply rolling restarts for all containers in dependency order.
		rollingFailed, rollingErr := performRollingRestart(
			log,
			ctx,
			allContainersToRestart,
			client,
			config,
			&cleanupImageInfos,
			progress,
			gitSession,
		)
		recordRestartOutcomes(log, progress, rollingFailed)

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
		failedStop, stoppedImages = stopContainersInReversedOrder(
			log,
			ctx,
			allContainersToRestart,
			client,
			config,
		)
		recordRestartOutcomes(log, progress, failedStop)

		failedStart = restartContainersInSortedOrder(
			log,
			ctx,
			allContainersToRestart,
			client,
			config,
			stoppedImages,
			&cleanupImageInfos,
			progress,
			gitSession,
		)
		recordRestartOutcomes(log, progress, failedStart)
	}

	// Run post-check lifecycle hooks if enabled to finalize the update process.
	// Passing nil lists the containers again, so the hooks run in the
	// replacements of updated containers rather than in the removed originals
	// held by the pre-update scan.
	if config.LifecycleHooks {
		log.Debug().Msg("Executing post-check lifecycle hooks")
		hooks.ExecutePostChecks(log, ctx, client, config, nil)
	}

	// A run-once Watchtower exits after the update, and a restart policy can
	// start it again at once. Delay the exit after a failed self-update pull
	// to prevent rapid restarts. A continuous Watchtower keeps running, and
	// its schedule spaces the next attempt.
	if watchtowerPullFailed && config.RunOnce {
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

// level records a dependency problem and returns its log level.
//
// A new problem gets [zerolog.WarnLevel] and a problem already recorded gets
// [zerolog.DebugLevel]. When the record is full, it starts over, so each
// problem that persists is reported once more.
//
// Parameters:
//   - parts: The problem kind followed by the values that identify it.
//
// Returns:
//   - zerolog.Level: [zerolog.WarnLevel] for a new problem, otherwise [zerolog.DebugLevel].
func (r *reportedProblems) level(parts ...string) zerolog.Level {
	key := strings.Join(parts, "\x00")

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, seen := r.seen[key]; seen {
		return zerolog.DebugLevel
	}

	if len(r.seen) >= maxReportedProblems {
		clear(r.seen)
	}

	r.seen[key] = struct{}{}

	return zerolog.WarnLevel
}

// hasSelfDependency checks if a container has a self-dependency in its Watchtower depends-on label.
// It uses the shared GetLinksFromWatchtowerLabel helper function for parsing and normalization.
func hasSelfDependency(c types.Container) bool {
	sourceContainer, ok := c.(*container.Container)
	if !ok {
		return false
	}

	links := container.GetLinksFromWatchtowerLabel(sourceContainer)

	return slices.Contains(links, c.Name())
}
