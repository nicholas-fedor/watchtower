package update

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	"github.com/nicholas-fedor/watchtower/internal/git/semver"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	"github.com/nicholas-fedor/watchtower/internal/update/changelog"
	"github.com/nicholas-fedor/watchtower/internal/update/cleanup"
	"github.com/nicholas-fedor/watchtower/internal/update/implicitrestart"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/rebuild"
	"github.com/nicholas-fedor/watchtower/internal/update/restart"
	"github.com/nicholas-fedor/watchtower/internal/update/selfupdate"
	"github.com/nicholas-fedor/watchtower/internal/update/sorter"
	"github.com/nicholas-fedor/watchtower/internal/update/staleness"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
)

// Client is what an update session needs from the Docker client. It also
// covers what the session's cleanup, rebuilds, restarts and self-update need.
type Client interface {
	ContainerLister
	cleanup.Client
	rebuild.ImageBuilder
	restart.Client
	selfupdate.RetireClient

	// IsContainerStale reports whether the container's image is outdated, with
	// the latest image ID and registry digest.
	IsContainerStale(
		ctx context.Context,
		target *container.Container,
		policy updateConfig.Policy,
	) (bool, container.ImageID, string, error)
	// GetImageAnnotations returns the OCI annotations of an image, or none
	// when they cannot be read.
	GetImageAnnotations(ctx context.Context, imageRef string) oci.Annotations
	// GetImageDiskUsage returns the daemon's image storage usage.
	GetImageDiskUsage(ctx context.Context) (containerClient.ImageDiskUsage, error)
}

// stalenessCheck is the outcome of one container's staleness check, before
// it is recorded in the session's progress.
type stalenessCheck struct {
	// checkErr is the error of the staleness check itself.
	checkErr error
	// verifyErr is the error of checking that a container to update can be
	// recreated.
	verifyErr error
	// newestImage is the latest image ID the check found, or the current
	// image ID of a fresh container.
	newestImage container.ImageID
	// latestAnns holds the OCI annotations of the latest image.
	latestAnns oci.Annotations
	// changelogVars holds the release tag and commit for the changelog URL.
	changelogVars provenance.ChangelogVars
	// stale reports whether the container's image is outdated.
	stale bool
	// willUpdate reports whether the container is stale and will be updated.
	willUpdate bool
	// resolveLatest reports whether the latest image's metadata was resolved
	// for the report.
	resolveLatest bool
}

// stalenessTally counts the results of one session's parallel staleness checks.
type stalenessTally struct {
	// failed counts the failed checks. A cooldown deferral is not counted.
	failed int
	// stale counts the containers whose image is outdated, whether or not
	// they will be updated.
	stale int
	// watchtowerPullFailed reports whether a check of Watchtower's own image
	// failed. A cooldown deferral does not count.
	watchtowerPullFailed bool
}

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

// defaultPullFailureDelay defines the default delay after a failed Watchtower self-update pull in run-once mode.
const defaultPullFailureDelay = 5 * time.Minute

// scanProblems holds the problems reported during this process. It records
// dependency problems and failed update checks. It also records stale
// containers whose configuration cannot be recreated. Keys include the
// container ID, so a recreated container is reported again. A container whose
// labels change is also recreated. A problem with a different cause is reported
// again too.
var scanProblems logging.ReportedProblems

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
//   - config: updateParams.Params specifying behavior like cleanup, restart, and filtering.
//   - rateLimiter: Rate limiter shared by registry requests. Must not be nil.
//   - gitClient: Git monitor client. Nil when Git monitoring is unused.
//
// Returns:
//   - *state.Report: Session report summarizing scanned, updated, and failed containers.
//   - []cleanup.RemovedImageInfo: Slice of cleaned image info to clean up after updates.
//   - error: Non-nil if listing or sorting fails, nil on success.
func update(
	log *zerolog.Logger,
	ctx context.Context,
	client Client,
	config updateParams.Params,
	rateLimiter *ratelimit.Limiter,
	gitClient ...*git.Client,
) (*state.Report, []cleanup.RemovedImageInfo, error) {
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

	// An old instance's container stops itself rather than run a session.
	// This catches one restarted despite the startup check.
	if old := currentOldInstance(allContainers, config.CurrentContainerID); old != nil {
		selfupdate.RetireOldInstance(log, ctx, client, old, config.DetachedTimeout())

		return nil, nil, errOldSelfDetected
	}

	// Initialize a slice to collect cleaned image info for cleanup after updates.
	cleanupImageInfos := []cleanup.RemovedImageInfo{}

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

	// Filter containers based on the provided filter (e.g., all, specific names).
	// Use a non-nil empty slice so lifecycle hooks can distinguish a valid empty
	// snapshot from an uninitialized list.
	filteredContainers := make([]*container.Container, 0, len(allContainers))

	for _, c := range allContainers {
		if config.Filter == nil || config.Filter(c) {
			filteredContainers = append(filteredContainers, c)
		}
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

	// A graph that cannot be built ends the session before any hook runs or
	// any image is checked.
	graph, skipped, err := checkDependencies(log, config, progress, allContainers, filteredContainers)
	if err != nil {
		return nil, cleanupImageInfos, err
	}

	// Run the pre-check lifecycle hooks just before the containers are
	// checked. A session that ends earlier runs neither these hooks nor the
	// post-checks.
	if config.LifecycleHooks {
		log.Debug().Msg("Executing pre-check lifecycle hooks")
		hooks.ExecutePreChecks(log, ctx, client, config.Filter, config.LifecycleUID, config.LifecycleGID, filteredContainers)
	}

	// A failed check of Watchtower's own image delays the end of a run-once
	// session.
	watchtowerPullFailed, err := checkStaleness(
		log,
		ctx,
		client,
		config,
		rateLimiter,
		gitSession,
		progress,
		filteredContainers,
	)
	if err != nil {
		return progress.Report(log), cleanupImageInfos, err
	}

	allContainersToRestart, err := planRestarts(
		log,
		ctx,
		client,
		config,
		gitSession,
		progress,
		graph,
		allContainers,
		filteredContainers,
		skipped,
	)
	if err != nil {
		return nil, cleanupImageInfos, err
	}

	// Perform updates and restarts, either with rolling restarts or in batches.
	err = restart.Run(
		log,
		ctx,
		allContainersToRestart,
		client,
		config,
		&cleanupImageInfos,
		progress,
	)
	if err != nil {
		return progress.Report(log), cleanupImageInfos, err
	}

	// Run post-check lifecycle hooks if enabled to finalize the update process.
	// Passing nil lists the containers again, so the hooks run in the
	// replacements of updated containers rather than in the removed originals
	// held by the pre-update scan.
	if config.LifecycleHooks {
		log.Debug().Msg("Executing post-check lifecycle hooks")
		hooks.ExecutePostChecks(
			log,
			ctx,
			client,
			config.Filter,
			config.LifecycleUID,
			config.LifecycleGID,
			nil,
		)
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

// currentOldInstance returns the current container when it is an old
// Watchtower instance with the watchtower-old- name prefix.
//
// Parameters:
//   - containers: Every listed container.
//   - currentID: The ID of Watchtower's own container, or empty when unknown.
//
// Returns:
//   - *container.Container: The current container, or nil when it is unknown,
//     not listed, or not an old Watchtower instance.
func currentOldInstance(containers []*container.Container, currentID container.ContainerID) *container.Container {
	if currentID == "" {
		return nil
	}

	i := slices.IndexFunc(containers, func(c *container.Container) bool { return c.ID() == currentID })
	if i < 0 {
		return nil
	}

	current := containers[i]
	if !current.IsWatchtower() || !container.IsSuperseded(current) {
		return nil
	}

	return current
}

// checkDependencies builds the session's dependency graph and records the
// monitored containers that cannot be ordered as skipped.
//
// A container with a dependency that names the container itself is skipped,
// and so is every member of a circular dependency. A cycle member that also
// names itself is reported once, as a cycle member. Each problem, and each
// depends-on entry that names no container, warns once and then logs at
// debug. Unmonitored containers join the graph so that a link naming one
// resolves to it, and is left alone, rather than to a monitored container.
//
// Parameters:
//   - log: Process logger.
//   - config: Update parameters.
//   - progress: Session progress that receives the skips.
//   - allContainers: Every listed container.
//   - filteredContainers: The monitored containers.
//
// Returns:
//   - *sorter.DependencyGraph: The graph that every dependency sort and the
//     implicit restarts of the session use.
//   - map[container.ContainerID]struct{}: IDs of the skipped containers, which
//     the session keeps out of its dependency sorts and implicit restarts.
//   - error: Non-nil when the graph cannot be built, for example when two
//     containers share a name. It wraps the error for a failed dependency sort.
func checkDependencies(
	log *zerolog.Logger,
	config updateParams.Params,
	progress *state.Progress,
	allContainers, filteredContainers []*container.Container,
) (*sorter.DependencyGraph, map[container.ContainerID]struct{}, error) {
	// The graph is built once, so each link resolves to the same container
	// throughout the session.
	graph, err := sorter.NewDependencyGraph(log,
		filteredContainers,
		unmonitoredContainers(allContainers, filteredContainers),
		config.UseComposeDependsOn,
	)
	if err != nil {
		return nil, nil, sortFailed(log, "Failed to build the dependency graph", err)
	}

	for _, link := range graph.UnresolvedDependsOn() {
		log.WithLevel(scanProblems.Level("unresolved depends-on", string(link.Container.ID()), link.Link)).
			Str("container", link.Container.Name()).
			Str("depends_on", link.Link).
			Msg(logging.UnresolvedDependsOn)
	}

	skipped := make(map[container.ContainerID]struct{})

	for _, c := range graph.CycleMembers() {
		skipped[c.ID()] = struct{}{}

		progress.AddSkipped(log, c, errCircularDependency, config)
		log.WithLevel(scanProblems.Level("circular dependency", string(c.ID()))).
			Str("container", c.Name()).
			Str("id", c.ID().ShortID()).
			Msg(logging.CircularDependency)
	}

	// A cycle member that also names itself is reported once, as a cycle
	// member, such as replicas whose depends-on names their own service.
	for _, c := range graph.SelfDependencies() {
		if _, reported := skipped[c.ID()]; reported {
			continue
		}

		skipped[c.ID()] = struct{}{}

		progress.AddSkipped(log, c, errSelfDependency, config)
		log.WithLevel(scanProblems.Level("self-dependency", string(c.ID()))).
			Str("container", c.Name()).
			Str("id", c.ID().ShortID()).
			Msg(logging.SelfDependency)
	}

	return graph, skipped, nil
}

// checkStaleness checks which monitored containers are stale and records each
// result in the session's progress.
//
// It skips a container already in progress, such as one the dependency phase
// skipped. It also skips a container pinned by digest. The others are checked
// in parallel, up to 20 at a time. Containers on the same upstream image
// share one release tag lookup. A container that will be updated is marked
// stale.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for the registry staleness checks.
//   - config: Update parameters.
//   - rateLimiter: Rate limiter shared by registry requests.
//   - gitSession: The session's Git monitor.
//   - progress: Session progress that receives each result.
//   - filteredContainers: The monitored containers to check.
//
// Returns:
//   - bool: True when a check of Watchtower's own image failed. A cooldown
//     deferral does not count.
//   - error: Non-nil when ctx ends before every check is recorded. It wraps
//     the context's error as "update canceled". The containers without a
//     result are reported as skipped with the context's error.
func checkStaleness(
	log *zerolog.Logger,
	ctx context.Context,
	client Client,
	config updateParams.Params,
	rateLimiter *ratelimit.Limiter,
	gitSession *rebuild.Session,
	progress *state.Progress,
	filteredContainers []*container.Container,
) (bool, error) {
	// Count the session's results for the summary. Workers update it under
	// resultMu, and the candidate loop runs before any worker starts.
	var (
		resultMu sync.Mutex
		tally    stalenessTally
	)

	// A canceled session ends with one error wherever the cancellation lands.
	// The containers not yet checked are reported as skipped. The partial
	// report then does not read as a complete scan.
	canceled := func() (bool, error) {
		progress.SkipUnrecorded(log, filteredContainers, fmt.Errorf("update skipped: %w", ctx.Err()), config)

		return false, fmt.Errorf("update canceled: %w", ctx.Err())
	}

	// Prepare containers for staleness checks, skipping already-processed ones.
	var candidates []*container.Container

	for _, sourceContainer := range filteredContainers {
		// Check for context cancellation to enable faster shutdown during long update cycles.
		if ctx.Err() != nil {
			return canceled()
		}

		// Skip containers already processed, such as the ones the dependency phase skipped.
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
			// A container whose image reference cannot be parsed is reported
			// as a failed update check.
			skipFailedUpdateCheck(log, clog, progress, sourceContainer,
				fmt.Errorf("%w: %w", errParseImageReference, err), config)

			tally.failed++

			continue
		}

		if isPinnedVal {
			// Skip staleness checks for pinned images and mark as scanned.
			clog.Debug().Msg("Skipping staleness check for pinned image")

			continue
		}

		candidates = append(candidates, sourceContainer)
	}

	// Check for context cancellation before launching parallel staleness checks.
	if ctx.Err() != nil {
		return canceled()
	}

	// Parallelize staleness checks with bounded concurrency.
	const maxConcurrentChecks = 20

	var checkGroup errgroup.Group
	checkGroup.SetLimit(maxConcurrentChecks)

	// One resolver per session so sibling containers on the same upstream image
	// share a single release tag lookup.
	tagResolver := changelog.NewResolver(changelog.NewDigestFetcher(config.Registry, rateLimiter))

	for _, sourceContainer := range candidates {
		checkGroup.Go(func() error {
			// Check for context cancellation to enable faster shutdown during long update cycles.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			clogVal := log.With().
				Str("container", sourceContainer.Name()).
				Str("image", sourceContainer.ImageName()).
				Logger()
			clog := &clogVal

			result, err := checkContainerStaleness(log, clog, ctx, client, config, gitSession, tagResolver, sourceContainer)
			if err != nil {
				return err
			}

			resultMu.Lock()
			defer resultMu.Unlock()

			recordStalenessCheck(log, clog, progress, config, sourceContainer, result, &tally)

			return nil
		})
	}

	err := checkGroup.Wait()
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Parallel staleness checks completed with error")

		// A worker returns an error only when the session is canceled. A
		// canceled check records nothing, so its container and those whose
		// check never ran are reported as skipped.
		return canceled()
	}

	// Log the summary of staleness checks, including total, stale, and failed counts.
	log.Debug().
		Int("total", len(filteredContainers)).
		Int("stale", tally.stale).
		Int("failed", tally.failed).
		Msg("Completed container staleness check")

	return tally.watchtowerPullFailed, nil
}

// checkContainerStaleness checks whether one container is stale and will be
// updated, without recording the result.
//
// A Watchtower container is not checked when self-updates are skipped. A
// Git-watched container is checked against its remote. Any other container is
// checked against its registry. For a container that will be updated, it also
// checks that the container can be recreated. It then resolves the latest
// image's metadata for the report.
//
// Parameters:
//   - log: Process logger.
//   - clog: Logger with the container's fields.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for the registry staleness check.
//   - config: Update parameters.
//   - gitSession: The session's Git monitor.
//   - tagResolver: The session's release tag lookup.
//   - sourceContainer: Container to check.
//
// Returns:
//   - stalenessCheck: The check's outcome.
//   - error: Non-nil only when ctx ends. A failed check is returned in the
//     outcome instead.
func checkContainerStaleness(
	log, clog *zerolog.Logger,
	ctx context.Context,
	client Client,
	config updateParams.Params,
	gitSession *rebuild.Session,
	tagResolver *changelog.Resolver,
	sourceContainer *container.Container,
) (stalenessCheck, error) {
	var (
		result     stalenessCheck
		newDigest  string
		gitWatched bool
	)

	// Determine if the container is stale and needs updating.
	switch {
	case sourceContainer.IsWatchtower() && config.SkipSelfUpdate:
		// Self-updates are skipped, so Watchtower's own container is not
		// checked and stays fresh.
	case gitSession.Watching(log, sourceContainer, config):
		// A Git-watched container is stale when its remote ref has advanced
		// past the revision it runs. The check returns a Git image ID for the
		// rebuild instead of a registry image.
		gitWatched = true

		result.stale, result.newestImage, result.checkErr = gitSession.Check(ctx, sourceContainer, config)
	default:
		// Any other container is checked against its registry. Unless no-pull
		// is set, the latest image is pulled and compared with the running
		// one. A cooldown holds a newer image back with a cooldown error. The
		// latest manifest digest feeds the report's release tag lookup.
		result.stale, result.newestImage, newDigest, result.checkErr = client.IsContainerStale(
			ctx,
			sourceContainer,
			config.Policy,
		)
	}

	// Only the session's own cancellation ends the scan. A check that
	// fails with a timeout of its own, such as a registry request
	// reaching its client timeout, is that container's check error.
	if result.checkErr != nil && ctx.Err() != nil {
		return stalenessCheck{}, fmt.Errorf("staleness check canceled: %w", ctx.Err())
	}

	// For fresh containers, set newestImage to current image ID for proper categorization.
	// (The cooldown decision happens in the image client's PullImage, after the
	// digest staleness check and before any layer is pulled.)
	if !result.stale {
		result.newestImage = sourceContainer.ImageID()
	}

	// Determine if the container should be updated based on staleness and
	// config. A Git-watched container is updated only through a rebuild.
	shouldUpdate := staleness.ShouldUpdate(sourceContainer, result.stale, config) &&
		(!gitWatched || rebuild.Allowed(sourceContainer, config.Policy))

	// Log when skipping Watchtower self-update in run-once mode.
	if result.stale && sourceContainer.IsWatchtower() && config.RunOnce {
		clog.Info().Msg(logging.SelfUpdateSkippedRunOnce)
	}

	// Verify the container's configuration if it's slated for update to
	// ensure recreation is possible.
	if result.checkErr == nil && shouldUpdate {
		result.verifyErr = sourceContainer.VerifyConfiguration()
	}

	result.willUpdate = result.stale && shouldUpdate && result.checkErr == nil && result.verifyErr == nil

	// Record the new image's OCI metadata so a notification can describe
	// what the container is moving to. This runs only for a container
	// that will actually be updated. The inspect and the optional
	// release tag lookup happen before the result is recorded, because a
	// registry round trip under the session's result lock would serialize
	// every parallel check behind one container's request.
	// A Git-watched container is excluded because the build path resolves
	// and logs its own changelog from the resolved tag afterwards.
	result.resolveLatest = result.willUpdate && !gitWatched

	if result.resolveLatest {
		latest := staleness.LatestImage{
			Annotations: client.GetImageAnnotations(ctx, sourceContainer.ImageName()),
			Digest:      newDigest,
		}

		result.changelogVars, result.latestAnns = staleness.ResolveLatestImageMeta(
			log,
			ctx,
			tagResolver,
			sourceContainer,
			config,
			latest,
		)
	}

	return result, nil
}

// recordStalenessCheck records one container's staleness check in the
// session's progress and tally.
//
// It marks the container stale when it will be updated. Callers hold the
// session's result lock.
//
// Parameters:
//   - log: Process logger.
//   - clog: Logger with the container's fields.
//   - progress: Session progress.
//   - config: Update parameters.
//   - sourceContainer: Container that was checked.
//   - result: The check's outcome.
//   - tally: The session's check counts.
func recordStalenessCheck(
	log, clog *zerolog.Logger,
	progress *state.Progress,
	config updateParams.Params,
	sourceContainer *container.Container,
	result stalenessCheck,
	tally *stalenessTally,
) {
	// Handle staleness check results, logging skips or adding to the progress report.
	switch {
	case result.checkErr != nil:
		// Skip containers with staleness check errors, marking them as
		// skipped. Every error except a cooldown deferral counts as a failed
		// check. A failed cooldown age lookup counts too.
		deferred := errors.Is(result.checkErr, containerImage.ErrImageCooldown)
		if !deferred {
			tally.failed++
		}

		cooldownErr, isCooldownError := errors.AsType[*containerImage.CooldownError](result.checkErr)

		// Each failure gets at most one line that reaches notifications.
		// A failure that its own check already logged in such a line gets
		// no second one.
		switch {
		case ratelimit.Is(result.checkErr):
			// One line per container per cycle. Log-based notifications
			// carry it, and the report template lists the container under
			// Failed instead. A Retry-After beyond the honor window was
			// already logged at warn by the retry loop.
			if !ratelimit.ExceedsHonorWindow(result.checkErr) {
				clog.Warn().
					Err(result.checkErr).
					Msg(logging.RateLimitRetriesExhausted)
			}

			progress.AddFailed(log, sourceContainer, result.checkErr, config)
		case isCooldownError:
			// The cooldown check logged the deferral or the failed lookup.
			// The *image.CooldownError carries the details for reports and
			// notifications.
			progress.AddSkipped(log, sourceContainer, result.checkErr, config)
			progress.SetCooldownInfo(log,
				sourceContainer.ID(),
				cooldownErr.Age,
				cooldownErr.Delay,
				cooldownErr.Remaining,
				cooldownErr.EligibleAt,
			)
		case errors.Is(result.checkErr, semver.ErrInvalidPolicy):
			// A container's labels cannot change, so the container ID
			// identifies an invalid label problem.
			progress.AddSkipped(log, sourceContainer, result.checkErr, config)
			clog.WithLevel(scanProblems.Level("invalid git semver policy", string(sourceContainer.ID()))).
				Msg(logging.InvalidGitSemverPolicy)
		case errors.Is(result.checkErr, provenance.ErrInvalidHost):
			// The container ID identifies the problem, as for an invalid
			// policy. The line carries no error field, so a credential in
			// the label stays out of log-based notifications.
			progress.AddSkipped(log, sourceContainer, result.checkErr, config)
			clog.WithLevel(scanProblems.Level("invalid git-host", string(sourceContainer.ID()))).
				Msg(logging.InvalidGitHost)
		default:
			skipFailedUpdateCheck(log, clog, progress, sourceContainer, result.checkErr, config)
		}

		// Watchtower's own image is checked only when the session does not
		// skip self-updates, so a failed check of it is a failed self-update
		// pull.
		if sourceContainer.IsWatchtower() && !deferred {
			tally.watchtowerPullFailed = true
		}
	case result.verifyErr != nil:
		tally.failed++

		progress.AddSkipped(log, sourceContainer, result.verifyErr, config)
		clog.WithLevel(scanProblems.Level("unrecreatable configuration", string(sourceContainer.ID()), result.verifyErr.Error())).
			Err(result.verifyErr).
			Msg(logging.ConfigNotRecreatable)
	default:
		// Log successful staleness check and add to scanned containers.
		clog.Debug().
			Bool("stale", result.stale).
			Str("newest_image", string(result.newestImage)).
			Msg("Checked container staleness")
		progress.AddScanned(log,
			sourceContainer,
			result.newestImage,
			config,
		)
	}

	// Write the pre-resolved metadata now that the session state is locked.
	if result.resolveLatest {
		staleness.ApplyLatestImageMeta(
			clog,
			progress,
			sourceContainer,
			config,
			result.changelogVars,
			result.latestAnns,
		)
	}

	// Update the container's stale status for dependency sorting.
	// Only mark as stale if the container should actually be updated.
	sourceContainer.SetStale(result.willUpdate)

	// Increment stale count for logging summary.
	if result.stale {
		tally.stale++
	}
}

// skipFailedUpdateCheck records a container whose update check failed as
// skipped, with the error as the reason. It warns the first time the container
// fails for that error and logs at debug while the same failure persists.
//
// Parameters:
//   - log: Process logger for the progress record.
//   - clog: Logger carrying the container and image fields.
//   - progress: Session progress that records the skip.
//   - c: Container whose check failed.
//   - err: Reason the check failed.
//   - config: Update parameters for the monitor-only check.
func skipFailedUpdateCheck(
	log, clog *zerolog.Logger,
	progress *state.Progress,
	c *container.Container,
	err error,
	config updateParams.Params,
) {
	progress.AddSkipped(log, c, err, config)
	clog.WithLevel(scanProblems.Level("failed update check", string(c.ID()), err.Error())).
		Err(err).
		Msg(logging.UpdateCheckFailed)
}

// planRestarts orders the monitored containers by dependency and decides
// which containers the session restarts, in restart order.
//
// The restart list holds the containers to update and those that restart
// because a container they depend on restarts. planRestarts runs the Git
// rebuilds of the stale Git-watched containers on that list. A container whose
// rebuild failed is no longer stale. The list is then worked out again without
// it. The list leaves out the containers that Compose already recreated and
// the no-restart Git builds. The containers the dependency phase skipped are
// kept out of the sorts and the implicit restarts. They are placed after the
// other monitored containers.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for the Git image builds.
//   - config: Update parameters.
//   - gitSession: The session's Git monitor.
//   - progress: Session progress that receives the rebuild results.
//   - graph: The session's dependency graph.
//   - allContainers: Every listed container.
//   - filteredContainers: The monitored containers, sorted in place.
//   - skipped: IDs of the containers the dependency phase skipped, the
//     self-dependents and cycle members.
//
// Returns:
//   - []*container.Container: The containers to restart, in restart order.
//   - error: Non-nil when a dependency sort fails.
func planRestarts(
	log *zerolog.Logger,
	ctx context.Context,
	client Client,
	config updateParams.Params,
	gitSession *rebuild.Session,
	progress *state.Progress,
	graph *sorter.DependencyGraph,
	allContainers, filteredContainers []*container.Container,
	skipped map[container.ContainerID]struct{},
) ([]*container.Container, error) {
	// Sort containers by dependencies to ensure correct update and restart order.
	// Skipped containers are placed after the others. Cycle members cannot be
	// ordered at all.
	// Every sort keeps only the graph's dependencies between the containers it
	// sorts, so leaving a container out cannot redirect a link to a different
	// container and form a cycle that the graph does not have.
	sortable := make([]*container.Container, 0, len(filteredContainers))
	unsortable := make([]*container.Container, 0, len(skipped))

	for _, c := range filteredContainers {
		if _, skip := skipped[c.ID()]; skip {
			unsortable = append(unsortable, c)
		} else {
			sortable = append(sortable, c)
		}
	}

	err := graph.Sort(log, sortable)
	if err != nil {
		return nil, sortFailed(log, "Failed to sort containers by dependencies", err)
	}

	// Write the order back, with the skipped containers last.
	copy(filteredContainers, sortable)
	copy(filteredContainers[len(sortable):], unsortable)

	// Collect all containers to restart: updates, and containers linked to
	// restarting ones, which restart without updating.
	allContainersToRestart := implicitrestart.Reconcile(log,
		graph,
		allContainers,
		filteredContainers,
		config,
		skipped,
	)

	// Sort containers to restart by dependencies to ensure correct update and restart order.
	err = graph.Sort(log, allContainersToRestart)
	if err != nil {
		return nil, sortFailed(log, "Failed to sort all containers to restart by dependencies", err)
	}

	// Log the number of containers prepared for restart.
	log.Debug().
		Int("restart_count", len(allContainersToRestart)).
		Msg("Prepared containers for restart")

	gitFailed := make(map[container.ContainerID]error)
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

	// Work out the implicit restarts again without the failed rebuilds, which are no longer stale.
	// Their dependents then stay running. The skipped containers stay out, as in the first pass.
	// No-restart builds stay out because every container in the plan is stopped.
	excluded := make(map[container.ContainerID]struct{}, len(skipped))
	maps.Copy(excluded, gitSession.NoRestartBuiltIDs(config))
	maps.Copy(excluded, skipped)

	allContainersToRestart = implicitrestart.Reconcile(
		log,
		graph,
		allContainers,
		filteredContainers,
		config,
		excluded,
	)

	err = graph.Sort(log, allContainersToRestart)
	if err != nil {
		return nil, sortFailed(log, "Failed to sort all containers to restart by dependencies", err)
	}

	// Compose already recreated the containers it applied. Do not recreate
	// them again from their inspect data.
	return gitSession.ExcludeApplied(allContainersToRestart), nil
}

// sortFailed logs a failure to order the containers by dependency and returns
// the error that ends the session.
//
// Parameters:
//   - log: Process logger. The failure is logged at debug.
//   - msg: Log message naming the failed step.
//   - err: The failure.
//
// Returns:
//   - error: err, wrapped in the error for a failed dependency sort.
func sortFailed(log *zerolog.Logger, msg string, err error) error {
	log.Debug().
		Err(err).
		Msg(msg)

	return fmt.Errorf("%w: %w", errSortDependenciesFailed, err)
}

// unmonitoredContainers returns the containers of allContainers that are not
// among the monitored containers.
//
// Parameters:
//   - allContainers: Every listed container.
//   - monitored: The monitored containers.
//
// Returns:
//   - []*container.Container: The other containers, in listed order.
func unmonitoredContainers(allContainers, monitored []*container.Container) []*container.Container {
	isMonitored := make(map[container.ContainerID]bool, len(monitored))
	for _, c := range monitored {
		isMonitored[c.ID()] = true
	}

	unmonitored := make([]*container.Container, 0, max(len(allContainers)-len(monitored), 0))

	for _, c := range allContainers {
		if !isMonitored[c.ID()] {
			unmonitored = append(unmonitored, c)
		}
	}

	return unmonitored
}
