package actions

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/distribution/reference"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/compose"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/git/apply"
	"github.com/nicholas-fedor/watchtower/internal/git/project"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	gitPkg "github.com/nicholas-fedor/watchtower/pkg/container/git"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// gitShortSHALen is the number of hex characters used in name:git-<shortsha> tags.
const gitShortSHALen = 12

// gitSession holds watcher results for one Update() invocation.
type gitSession struct {
	client   *git.Client
	apply    compose.Applier
	checkout func(ctx context.Context, dir, commit string, opts project.CheckoutOptions) error
	mu       sync.Mutex
	results  map[types.ContainerID]git.CheckResult
	built    map[types.ContainerID]types.ImageID
	applied  map[types.ContainerID]struct{}
}

// composeMember is one stale container and the check result that selected its commit.
type composeMember struct {
	container types.Container
	result    git.CheckResult
}

// composeBatch is one on-disk Compose project to checkout and apply.
type composeBatch struct {
	ref     compose.ProjectRef
	members []composeMember
}

// newGitSession constructs an empty session for one Update() call.
//
// Parameters:
//   - client: Git monitor client. May be unused when no container is monitored.
//
// Returns:
//   - *gitSession: Session with empty result and built maps.
func newGitSession(client *git.Client) *gitSession {
	return &gitSession{
		client:   client,
		apply:    compose.NewSDK(),
		checkout: project.Checkout,
		results:  make(map[types.ContainerID]git.CheckResult),
		built:    make(map[types.ContainerID]types.ImageID),
		applied:  make(map[types.ContainerID]struct{}),
	}
}

// store records a monitor result for containerID.
//
// Parameters:
//   - containerID: Container identity.
//   - result: Check outcome from the Git client.
//
// Returns:
//   - none.
func (s *gitSession) store(containerID types.ContainerID, result git.CheckResult) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.results[containerID] = result
}

// result returns the stored check for containerID.
//
// Parameters:
//   - containerID: Container identity.
//
// Returns:
//   - git.CheckResult: Stored result.
//   - bool: True when a result was stored.
func (s *gitSession) result(containerID types.ContainerID) (git.CheckResult, bool) {
	if s == nil {
		return git.CheckResult{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, ok := s.results[containerID]

	return result, ok
}

// watching reports whether Git monitoring applies to c in this session.
//
// Parameters:
//   - log: Process logger.
//   - c: Container to inspect.
//   - params: Update parameters.
//
// Returns:
//   - bool: True when the session should check Git instead of the registry.
func (s *gitSession) watching(log *zerolog.Logger, c types.Container, params types.UpdateParams) bool {
	if s == nil || s.client == nil {
		return false
	}

	return gitPkg.ShouldMonitor(log, c, params)
}

// check runs a Git staleness check and stores the result.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - c: Associated container.
//   - params: Update parameters including cooldown.
//
// Returns:
//   - bool: True when the remote ref has advanced past the known running revision.
//   - types.ImageID: Synthetic Git image id used by the update pipeline.
//   - error: Non-nil on monitor or cooldown failure.
func (s *gitSession) check(
	ctx context.Context,
	c types.Container,
	params types.UpdateParams,
) (bool, types.ImageID, error) {
	result, err := git.CheckContainer(ctx, s.client, c, params)
	if err != nil {
		return false, "", fmt.Errorf("git check: %w", err)
	}

	s.store(c.ID(), result)

	if !result.Stale {
		return false, c.ImageID(), nil
	}

	err = container.CheckLocalImageCooldown(c, params)
	if err != nil {
		return false, "", fmt.Errorf("git cooldown: %w", err)
	}

	newest := types.ImageID("git:" + result.Commit)
	if newest == "git:" {
		newest = types.ImageID("git:unknown")
	}

	return true, newest, nil
}

// prepareRebuilds applies stale Git-associated containers.
//
// compose-dir or --compose-project selects a local path context. Watchtower
// checks out the Compose project directory, then runs compose up or compose build.
// Git labels alone use a Git URL context. The Docker daemon clones that URL.
//
// Failures are recorded in failed and those containers are unmarked stale so
// the running instance is left untouched.
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
func (s *gitSession) prepareRebuilds(
	log *zerolog.Logger,
	ctx context.Context,
	docker container.Client,
	containers []types.Container,
	params types.UpdateParams,
	progress *session.Progress,
	failed map[types.ContainerID]error,
) {
	if s == nil || s.client == nil {
		return
	}

	var remote []types.Container

	batches := map[string]*composeBatch{}

	for _, c := range containers {
		if !s.needsApply(c, params) {
			continue
		}

		result, _ := s.result(c.ID())

		ref, err := compose.ResolveProjectDir(containerLabels(c), params.ComposeProjects)
		if err != nil {
			failed[c.ID()] = err
			c.SetStale(false)
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

		batch := batches[ref.Dir]
		if batch == nil {
			batch = &composeBatch{ref: ref}
			batches[ref.Dir] = batch
		}

		batch.members = append(batch.members, composeMember{container: c, result: result})
	}

	for _, batch := range batches {
		s.applyCompose(log, ctx, batch, params, progress, failed)
	}

	for _, c := range remote {
		result, _ := s.result(c.ID())

		err := s.buildOne(log, ctx, docker, c, result, params, progress)
		if err != nil {
			failed[c.ID()] = err
			c.SetStale(false)
			log.Warn().
				Err(err).
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Msg("Git build failed. Leaving running container untouched")
		}
	}
}

// needsApply reports whether c is a stale Git-associated container that may be rebuilt.
//
// Parameters:
//   - c: Candidate container.
//   - params: Update parameters.
//
// Returns:
//   - bool: True when a Git apply backend should run.
func (s *gitSession) needsApply(c types.Container, params types.UpdateParams) bool {
	if c == nil || !c.IsStale() {
		return false
	}

	result, ok := s.result(c.ID())
	if !ok || !result.Stale {
		return false
	}

	return !c.IsNoPull(params) && !c.IsMonitorOnly(params)
}

// buildOne builds one image from a Git URL context and stamps the container.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Cancellation and timeout.
//   - docker: Docker client.
//   - c: Stale associated container.
//   - result: Monitor result with the target commit.
//   - params: Update parameters.
//   - progress: Session progress, or nil.
//
// Returns:
//   - error: Non-nil when association, remote URL, or the host build fails.
func (s *gitSession) buildOne(
	log *zerolog.Logger,
	ctx context.Context,
	docker container.Client,
	c types.Container,
	result git.CheckResult,
	params types.UpdateParams,
	progress *session.Progress,
) error {
	assoc, associated := gitPkg.ResolveAssociation(c, params)
	if !associated {
		return errGitNotAssociated
	}

	dockerfile, err := apply.SafeRelPath(assoc.Dockerfile)
	if err != nil {
		return fmt.Errorf("git dockerfile: %w", err)
	}

	if dockerfile == "" {
		dockerfile = container.DefaultGitDockerfile
	}

	remote, err := apply.RemoteContext(assoc.Repo, result.Commit, assoc.Context)
	if err != nil {
		return fmt.Errorf("git remote context: %w", err)
	}

	user, token := s.client.BuildAuth(assoc.Repo)

	remote, err = apply.WithAuth(remote, user, token)
	if err != nil {
		return fmt.Errorf("git remote auth: %w", err)
	}

	originalName := c.ImageName()
	tag := gitImageTag(originalName, result.Commit)
	tags := []string{tag}

	if originalName != "" && originalName != tag {
		tags = append(tags, originalName)
	}

	imageID, err := docker.BuildRemoteImage(ctx, remote, dockerfile, tags)
	if err != nil {
		return fmt.Errorf("build git image: %w", err)
	}

	if concrete, ok := c.(*container.Container); ok {
		container.ApplyGitAssociation(concrete, assoc, gitPkg.PersistWatch(c))

		concrete.SetImageName(tag)
		container.ApplyGitStamp(concrete, result.Commit, result.Tag)
	}

	s.mu.Lock()
	s.built[c.ID()] = imageID
	s.mu.Unlock()

	if progress != nil {
		progress.SetLatestImage(log, c.ID(), imageID)
		progress.RefreshChangelog(c, params, result.Tag, result.Commit)
	}

	log.Info().
		Str("container", c.Name()).
		Str("image", tag).
		Str("commit", result.Commit).
		Msg("Built image from Git URL context")

	return nil
}

// skipRecreate reports whether inspect recreate should be skipped for c.
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
func (s *gitSession) skipRecreate(c types.Container, params types.UpdateParams) bool {
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

// excludeApplied drops containers already recreated by Compose apply.
//
// Parameters:
//   - containers: Full candidate list.
//
// Returns:
//   - []types.Container: Containers that may still use inspect recreate.
func (s *gitSession) excludeApplied(containers []types.Container) []types.Container {
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

// excludeNoRestart drops Git-built containers from the recreate list when no-restart is set.
//
// Parameters:
//   - containers: Full candidate list.
//   - params: Update parameters.
//
// Returns:
//   - []types.Container: Containers that may still be recreated.
func (s *gitSession) excludeNoRestart(
	containers []types.Container,
	params types.UpdateParams,
) []types.Container {
	if s == nil || !params.NoRestart {
		return containers
	}

	kept := make([]types.Container, 0, len(containers))

	for _, c := range containers {
		if s.skipRecreate(c, params) {
			continue
		}

		kept = append(kept, c)
	}

	return kept
}

// gitImageTag builds name:git-<shortsha> from the current image name.
//
// Parameters:
//   - imageName: Current image reference.
//   - commit: Full commit SHA.
//
// Returns:
//   - string: Tag applied to the built image.
func gitImageTag(imageName, commit string) string {
	short := commit
	if len(short) > gitShortSHALen {
		short = short[:gitShortSHALen]
	}

	named, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		base := imageName
		if i := strings.LastIndex(base, ":"); i > 0 && !strings.Contains(base[i:], "/") {
			base = base[:i]
		}

		return base + ":git-" + short
	}

	return named.Name() + ":git-" + short
}
