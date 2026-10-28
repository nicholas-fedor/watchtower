package rebuild

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	composeApply "github.com/nicholas-fedor/watchtower/internal/compose/apply"
	composeProject "github.com/nicholas-fedor/watchtower/internal/compose/project"
	containerClient "github.com/nicholas-fedor/watchtower/internal/container/client"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/git/project"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Applier loads a Compose project and applies selected services.
type Applier interface {
	// Apply loads the project, merges Labels, and runs compose up or build.
	//
	// Parameters:
	//   - ctx: Cancellation and timeout.
	//   - req: Project, services, labels, and build-only flag.
	//
	// Returns:
	//   - []composeApply.Container: Service instances after apply.
	//   - error: Non-nil when load or apply fails.
	Apply(ctx context.Context, req composeApply.Request) ([]composeApply.Container, error)
}

// The Compose client applies the projects that a session checks out.
var _ Applier = (*composeApply.Client)(nil)

// Session holds watcher results for one Update() invocation.
type Session struct {
	client   *git.Client
	apply    Applier
	checkout func(ctx context.Context, dir, commit string, opts project.CheckoutOptions) error
	mu       sync.Mutex
	results  map[types.ContainerID]git.CheckResult
	built    map[types.ContainerID]types.ImageID
	applied  map[types.ContainerID]struct{}
}

// NewSession constructs an empty session for one Update() call.
//
// Parameters:
//   - client: Git monitor client. May be unused when no container is monitored.
//
// Returns:
//   - *Session: Session with empty result and built maps.
func NewSession(client *git.Client) *Session {
	return &Session{
		client:   client,
		apply:    composeApply.NewClient(),
		checkout: project.Checkout,
		results:  make(map[types.ContainerID]git.CheckResult),
		built:    make(map[types.ContainerID]types.ImageID),
		applied:  make(map[types.ContainerID]struct{}),
	}
}

// Watching reports whether Git monitoring applies to a container in this session.
//
// Parameters:
//   - log: Process logger.
//   - c: Container to inspect.
//   - params: Update parameters.
//
// Returns:
//   - bool: True when the session should check Git instead of the registry.
func (s *Session) Watching(log *zerolog.Logger, c types.Container, params types.UpdateParams) bool {
	if s == nil || s.client == nil {
		return false
	}

	return provenance.ShouldMonitor(log, c, params)
}

// PrepareRebuilds applies stale Git-associated containers.
//
// The compose-dir label or the compose-project configuration option selects a
// local path context. Watchtower checks out the Compose project directory, then
// runs compose up or compose build.
// Git labels alone use a Git URL context. The Docker daemon clones that URL.
//
// Failures are recorded in failed and those containers are unmarked stale so
// the current session does not stop them. Checkout and build failures leave the
// running instance untouched. Compose apply failures report that Docker Compose
// may have partially recreated the batch. An unreadable compose project
// directory is not recorded in failed. That container is unmarked stale, marked
// skipped when progress is set, and is not rebuilt from a Git URL.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Cancellation and timeout.
//   - docker: Docker client used for ImageBuild.
//   - containers: Candidates already marked stale.
//   - params: Update parameters.
//   - progress: Session progress, or nil.
//   - failed: Map to record per-container build errors.
//
// Returns:
//   - none.
func (s *Session) PrepareRebuilds(
	log *zerolog.Logger,
	ctx context.Context,
	docker containerClient.Client,
	containers []types.Container,
	params types.UpdateParams,
	progress *state.Progress,
	failed map[types.ContainerID]error,
) {
	if s == nil || s.client == nil {
		return
	}

	var remote []types.Container

	var batches composeBatches

	ordered := orderedComposeContainers(log, containers, params.UseComposeDependsOn)

	for _, c := range ordered {
		if !s.needsApply(c, params) {
			continue
		}

		result, _ := s.result(c.ID())

		ref, err := composeProject.ResolveProjectDir(containerLabels(c), params.ComposeProjects)
		if err != nil {
			c.SetStale(false)

			if progress != nil {
				progress.AddSkipped(log, c, err, params)
			}

			log.Warn().
				Err(err).
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Msg("Compose project directory is not readable. Leaving the running container untouched")

			continue
		}

		if ref.Dir == "" {
			remote = append(remote, c)

			continue
		}

		batches.add(ref, composeMember{container: c, result: result})
	}

	for _, batch := range batches.inDependencyOrder(log, containers, params.UseComposeDependsOn) {
		s.applyCompose(log, ctx, batch, params, progress, failed)
	}

	for _, c := range remote {
		result, _ := s.result(c.ID())

		err := s.build(log, ctx, docker, c, result, params, progress)
		if err != nil {
			failed[c.ID()] = err
			c.SetStale(false)
			withGitMeta(log.Warn().
				Err(err).
				Str("container", c.Name()).
				Str("image", c.ImageName()),
				c, params, result.Tag, result.Commit).
				Msg("Git build failed. Leaving running container untouched")
		}
	}
}

// SkipRecreate reports whether recreating a container from its inspect data should be skipped.
//
// Compose-applied containers are always skipped. Git URL context builds are
// skipped only when no-restart is set. Watchtower itself still uses the inspect path.
//
// Parameters:
//   - c: Container to inspect.
//   - params: Update parameters.
//
// Returns:
//   - bool: True when this session already replaced or must not replace c.
func (s *Session) SkipRecreate(c types.Container, params types.UpdateParams) bool {
	if s == nil || c.IsWatchtower() {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.applied[c.ID()]; ok {
		return true
	}

	if !params.NoRestart {
		return false
	}

	_, built := s.built[c.ID()]

	return built
}

// ExcludeApplied drops containers already recreated by Compose apply.
//
// Parameters:
//   - containers: Full candidate list.
//
// Returns:
//   - []types.Container: Containers that may still use inspect recreate.
func (s *Session) ExcludeApplied(containers []types.Container) []types.Container {
	if s == nil {
		return containers
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.applied) == 0 {
		return containers
	}

	kept := make([]types.Container, 0, len(containers))

	for _, c := range containers {
		if _, ok := s.applied[c.ID()]; ok {
			continue
		}

		kept = append(kept, c)
	}

	return kept
}

// ExcludeNoRestart drops Git-built containers from the recreate list when no-restart is set.
//
// Parameters:
//   - containers: Full candidate list.
//   - params: Update parameters.
//
// Returns:
//   - []types.Container: Containers that may still be recreated.
func (s *Session) ExcludeNoRestart(
	containers []types.Container,
	params types.UpdateParams,
) []types.Container {
	if s == nil || !params.NoRestart {
		return containers
	}

	kept := make([]types.Container, 0, len(containers))

	for _, c := range containers {
		if s.SkipRecreate(c, params) {
			continue
		}

		kept = append(kept, c)
	}

	return kept
}

// NoRestartBuiltIDs returns a snapshot of container IDs built during a no-restart update.
//
// Parameters:
//   - params: Update parameters.
//
// Returns:
//   - map[types.ContainerID]struct{}: Built container IDs, or nil when restart is enabled.
func (s *Session) NoRestartBuiltIDs(params types.UpdateParams) map[types.ContainerID]struct{} {
	if s == nil || !params.NoRestart {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ids := make(map[types.ContainerID]struct{}, len(s.built))
	for id := range s.built {
		ids[id] = struct{}{}
	}

	return ids
}

// store records the Git check result of a container.
//
// Parameters:
//   - containerID: Container identity.
//   - result: Check outcome from the Git client.
//
// Returns:
//   - none.
func (s *Session) store(containerID types.ContainerID, result git.CheckResult) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.results[containerID] = result
}

// result returns the stored Git check result of a container.
//
// Parameters:
//   - containerID: Container identity.
//
// Returns:
//   - git.CheckResult: Stored result.
//   - bool: True when a result was stored.
func (s *Session) result(containerID types.ContainerID) (git.CheckResult, bool) {
	if s == nil {
		return git.CheckResult{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, ok := s.results[containerID]

	return result, ok
}

// needsApply reports whether a container is a stale Git-associated container that may be rebuilt.
//
// Parameters:
//   - c: Candidate container.
//   - params: Update parameters.
//
// Returns:
//   - bool: True when a Git apply backend should run.
func (s *Session) needsApply(c types.Container, params types.UpdateParams) bool {
	if c == nil || !c.IsStale() {
		return false
	}

	result, ok := s.result(c.ID())
	if !ok || !result.Stale {
		return false
	}

	return !c.IsNoPull(params) && !c.IsMonitorOnly(params)
}
