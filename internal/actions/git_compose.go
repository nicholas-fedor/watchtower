package actions

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/compose"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/git/project"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	gitPkg "github.com/nicholas-fedor/watchtower/pkg/container/git"
	"github.com/nicholas-fedor/watchtower/pkg/session"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// applyCompose checkouts a Compose project once and applies stale associated services.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Cancellation and timeout.
//   - batch: Containers that share one project directory.
//   - params: Update parameters including stash and no-restart.
//   - progress: Session progress, or nil.
//   - failed: Map to record per-container apply errors.
//
// Returns:
//   - none.
func (s *gitSession) applyCompose(
	log *zerolog.Logger,
	ctx context.Context,
	batch *composeBatch,
	params types.UpdateParams,
	progress *session.Progress,
	failed map[types.ContainerID]error,
) {
	if batch == nil || len(batch.containers) == 0 {
		return
	}

	services := make([]string, 0, len(batch.containers))
	labels := make(map[string]map[string]string, len(batch.containers))
	byService := make(map[string]types.Container, len(batch.containers))

	for _, c := range batch.containers {
		name := compose.GetServiceName(log, containerLabels(c))
		if name == "" {
			failed[c.ID()] = errGitComposeService
			c.SetStale(false)
			log.Warn().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Msg("Compose apply skipped. Container has no compose service label")

			continue
		}

		services = append(services, name)
		byService[name] = c
		labels[name] = composeServiceLabels(c, params, batch.result)
	}

	if len(services) == 0 {
		return
	}

	checkout := s.checkout
	if checkout == nil {
		checkout = project.Checkout
	}

	opts := project.CheckoutOptions{Stash: params.GitComposeStash}
	if s.client != nil {
		opts.AuthFor = s.client.Auth
		opts.CABundle, opts.InsecureSkipTLS = s.client.TLSSettings()
	}

	err := checkout(ctx, batch.ref.Dir, batch.result.Commit, opts)
	if err != nil {
		failComposeBatch(log, batch, failed, fmt.Errorf("compose checkout: %w", err))

		return
	}

	applier := s.apply
	if applier == nil {
		applier = compose.NewSDK()
	}

	applied, err := applier.Apply(ctx, compose.Request{
		Ref:       batch.ref,
		Services:  services,
		Labels:    labels,
		BuildOnly: params.NoRestart,
	})
	if err != nil {
		failComposeBatch(log, batch, failed, fmt.Errorf("compose apply: %w", err))

		return
	}

	appliedByService := make(map[string]compose.Container, len(applied))
	for _, item := range applied {
		appliedByService[item.Service] = item
	}

	for _, name := range services {
		c := byService[name]

		item, ok := appliedByService[name]
		if !ok {
			failed[c.ID()] = errGitComposeInstance
			c.SetStale(false)
			log.Warn().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Str("service", name).
				Msg("Compose apply omitted service. Leaving running container untouched")

			continue
		}

		s.recordCompose(c, item, params, batch.result, progress, log)
	}

	log.Info().
		Str("project", batch.ref.Name).
		Str("dir", batch.ref.Dir).
		Str("commit", batch.result.Commit).
		Strs("services", services).
		Bool("build_only", params.NoRestart).
		Msg("Applied Compose project from Git")
}

// recordCompose records a successful Compose apply for one container.
//
// Parameters:
//   - c: Stale associated container.
//   - item: Compose instance after apply.
//   - params: Update parameters.
//   - result: Monitor result used for stamps.
//   - progress: Session progress, or nil.
//   - log: Process logger.
//
// Returns:
//   - none.
func (s *gitSession) recordCompose(
	c types.Container,
	item compose.Container,
	params types.UpdateParams,
	result git.CheckResult,
	progress *session.Progress,
	log *zerolog.Logger,
) {
	assoc, _ := gitPkg.ResolveAssociation(c, params)
	if concrete, ok := c.(*container.Container); ok {
		container.ApplyGitAssociation(concrete, assoc, gitPkg.PersistWatch(c))
		container.ApplyGitStamp(concrete, result.Commit, result.Tag)
	}

	s.mu.Lock()
	if params.NoRestart {
		if item.ImageID != "" {
			s.built[c.ID()] = item.ImageID
		} else {
			s.built[c.ID()] = types.ImageID("git:" + result.Commit)
		}
	} else {
		s.applied[c.ID()] = struct{}{}
		if item.ImageID != "" {
			s.built[c.ID()] = item.ImageID
		}
	}
	s.mu.Unlock()

	if progress == nil {
		return
	}

	if item.ImageID != "" {
		progress.SetLatestImage(log, c.ID(), item.ImageID)
	}

	progress.RefreshChangelog(c, params, result.Tag, result.Commit)

	if item.ID == "" {
		return
	}

	status, ok := (*progress)[c.ID()]
	if !ok || status == nil {
		return
	}

	status.SetNewContainerID(item.ID)
}

// failComposeBatch records an apply error on every container in the batch still pending.
//
// Parameters:
//   - log: Process logger.
//   - batch: Project batch.
//   - failed: Map to record errors.
//   - err: Apply or checkout error.
//
// Returns:
//   - none.
func failComposeBatch(
	log *zerolog.Logger,
	batch *composeBatch,
	failed map[types.ContainerID]error,
	err error,
) {
	for _, c := range batch.containers {
		if _, exists := failed[c.ID()]; exists {
			continue
		}

		failed[c.ID()] = err
		c.SetStale(false)
		log.Warn().
			Err(err).
			Str("container", c.Name()).
			Str("image", c.ImageName()).
			Str("dir", batch.ref.Dir).
			Msg("Compose apply failed. Leaving running container untouched")
	}
}

// containerLabels returns the container config labels, or nil.
//
// Parameters:
//   - c: Container to inspect.
//
// Returns:
//   - map[string]string: Label map.
func containerLabels(c types.Container) map[string]string {
	if c == nil {
		return nil
	}

	info := c.ContainerInfo()
	if info == nil || info.Config == nil {
		return nil
	}

	return info.Config.Labels
}

// composeServiceLabels is the stamp and association labels written onto new Compose containers.
//
// Parameters:
//   - c: Source container.
//   - params: Update parameters.
//   - result: Monitor result.
//
// Returns:
//   - map[string]string: Labels merged into the compose service.
func composeServiceLabels(c types.Container, params types.UpdateParams, result git.CheckResult) map[string]string {
	out := map[string]string{
		gitPkg.LastCommitLabel: result.Commit,
	}

	if result.Tag != "" {
		out[gitPkg.LastTagLabel] = result.Tag
	}

	assoc, ok := gitPkg.ResolveAssociation(c, params)
	if ok && assoc.Repo != "" {
		out[gitPkg.RepoLabel] = assoc.Repo

		if assoc.Ref != "" {
			out[gitPkg.RefLabel] = assoc.Ref
		}

		if assoc.Policy != "" {
			out[gitPkg.SemverPolicyLabel] = assoc.Policy
		}

		if assoc.Host != "" {
			out[gitPkg.HostLabel] = assoc.Host
		}

		if assoc.Dockerfile != "" {
			out[gitPkg.DockerfileLabel] = assoc.Dockerfile
		}

		if assoc.Context != "" {
			out[gitPkg.ContextLabel] = assoc.Context
		}

		if gitPkg.PersistWatch(c) {
			out[gitPkg.WatchLabel] = "true"
		}
	}

	if dir, present := c.GetLabel(gitPkg.ComposeDirLabel); present && dir != "" {
		out[gitPkg.ComposeDirLabel] = dir
	}

	return out
}
