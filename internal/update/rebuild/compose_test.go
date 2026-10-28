package rebuild

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	composeApply "github.com/nicholas-fedor/watchtower/internal/compose/apply"
	composeLabels "github.com/nicholas-fedor/watchtower/internal/compose/labels"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/git/project"
	mockRebuild "github.com/nicholas-fedor/watchtower/internal/update/rebuild/mocks"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// writeComposeProjectDir writes a Compose project with two built services to a
// temporary directory.
//
// Parameters:
//   - t: Test the directory belongs to.
//
// Returns:
//   - string: The project directory.
func writeComposeProjectDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  api:\n    build: .\n  worker:\n    build: ./worker\n"), 0o600))

	return dir
}

// TestSession_applyCompose verifies how stale Compose projects are checked out
// and applied, and the order they are applied in. It also checks how their
// results and failures are recorded.
func TestSession_applyCompose(t *testing.T) {
	t.Parallel()

	t.Run("checkouts once and applies stale services that share a project dir", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)

		var got composeApply.Request

		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Run(func(ctx context.Context, req composeApply.Request) {
				deadline, ok := ctx.Deadline()
				assert.True(t, ok)
				assert.LessOrEqual(t, time.Until(deadline), time.Second)

				got = req
			}).
			Return([]composeApply.Container{
				{Service: "api", ID: "new-api", ImageID: "sha256:api"},
				{Service: "worker", ID: "new-worker", ImageID: "sha256:worker"},
			}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier

		var checkouts int

		sess.checkout = func(ctx context.Context, gotDir, commit string, opts project.CheckoutOptions) error {
			deadline, ok := ctx.Deadline()
			assert.True(t, ok)
			assert.LessOrEqual(t, time.Until(deadline), time.Second)

			checkouts++

			assert.Equal(t, dir, gotDir)
			assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", commit)
			assert.True(t, opts.Stash)
			require.NotNil(t, opts.AuthFor)

			return nil
		}

		api := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeProjectLabel: "webstack",
			composeLabels.ComposeServiceLabel: "api",
		})
		worker := gitAssociatedContainer("worker", "/worker", "webstack-worker:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeProjectLabel: "webstack",
			composeLabels.ComposeServiceLabel: "worker",
		})

		api.SetStale(true)
		worker.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "0123456789abcdef0123456789abcdef01234567", Tag: "v1.2.3"}
		sess.store(api.ID(), result)
		sess.store(worker.ID(), result)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(
			newTestLogger(t),
			t.Context(),
			docker,
			[]types.Container{api, worker},
			types.UpdateParams{EnableGitMonitoring: true, GitComposeStash: true},
			nil,
			failed,
		)

		assert.Empty(t, failed)
		assert.Equal(t, 1, checkouts)
		assert.Equal(t, dir, got.Ref.Dir)
		assert.ElementsMatch(t, []string{"api", "worker"}, got.Services)
		assert.False(t, got.BuildOnly)
		assert.Equal(t, result.Commit, got.Labels["api"][labels.GitLastCommit])
		assert.Contains(t, sess.applied, api.ID())
		assert.Contains(t, sess.applied, worker.ID())
		assert.True(t, sess.SkipRecreate(api, types.UpdateParams{}))
	})

	t.Run("separates projects that share a directory and config identity", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		override := filepath.Join(dir, "override.yml")
		require.NoError(t, os.WriteFile(override, []byte("services: {}\n"), 0o600))

		applier := mockRebuild.NewMockApplier(t)

		var requests []composeApply.Request

		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, req composeApply.Request) ([]composeApply.Container, error) {
				requests = append(requests, req)

				return []composeApply.Container{{
					Service: req.Services[0],
					Name:    "/" + req.Ref.Name + "-api",
					ID:      types.ContainerID("new-" + req.Ref.Name),
					ImageID: types.ImageID("sha256:" + req.Ref.Name),
				}}, nil
			}).
			Twice()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier

		var checkouts int

		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			checkouts++

			return nil
		}

		alpha := gitAssociatedContainer("alpha", "alpha-api", "alpha:latest", map[string]string{
			labels.ComposeDir:                     dir,
			composeLabels.ComposeProjectLabel:     "alpha",
			composeLabels.ComposeServiceLabel:     "api",
			composeLabels.ComposeConfigFilesLabel: "compose.yaml",
		})
		beta := gitAssociatedContainer("beta", "beta-api", "beta:latest", map[string]string{
			labels.ComposeDir:                     dir,
			composeLabels.ComposeProjectLabel:     "beta",
			composeLabels.ComposeServiceLabel:     "api",
			composeLabels.ComposeConfigFilesLabel: "override.yml",
		})

		alpha.SetStale(true)
		beta.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "abc"}
		sess.store(alpha.ID(), result)
		sess.store(beta.ID(), result)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{alpha, beta}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.Empty(t, failed)
		assert.Equal(t, 2, checkouts)
		require.Len(t, requests, 2)
		assert.NotEqual(t, requests[1].Ref.Name, requests[0].Ref.Name)
		assert.NotEqual(t, requests[1].Ref.ConfigFiles, requests[0].Ref.ConfigFiles)
		assert.Contains(t, sess.applied, alpha.ID())
		assert.Contains(t, sess.applied, beta.ID())
	})

	t.Run("applies dependent Compose projects after their dependencies", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)

		var order []string

		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, req composeApply.Request) ([]composeApply.Container, error) {
				order = append(order, req.Ref.Name)

				return []composeApply.Container{{
					Service: req.Services[0],
					Name:    "/" + req.Ref.Name + "-api",
					ID:      types.ContainerID("new-" + req.Ref.Name),
					ImageID: types.ImageID("sha256:" + req.Ref.Name),
				}}, nil
			}).
			Twice()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		provider := gitAssociatedContainer("provider", "alpha-api", "alpha:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeProjectLabel: "alpha",
			composeLabels.ComposeServiceLabel: "api",
		})
		dependent := gitAssociatedContainer("dependent", "beta-api", "beta:latest", map[string]string{
			labels.ComposeDir:                           dir,
			composeLabels.ComposeProjectLabel:           "beta",
			composeLabels.ComposeServiceLabel:           "api",
			"com.centurylinklabs.watchtower.depends-on": "alpha-api",
		})

		provider.SetStale(true)
		dependent.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "abc"}
		sess.store(provider.ID(), result)
		sess.store(dependent.ID(), result)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{dependent, provider}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.Empty(t, failed)
		assert.Equal(t, []string{"alpha", "beta"}, order)
	})

	t.Run("applies a dependency project before a project whose first service is independent", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)

		var order []string

		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			RunAndReturn(func(_ context.Context, req composeApply.Request) ([]composeApply.Container, error) {
				order = append(order, req.Ref.Name)

				items := make([]composeApply.Container, 0, len(req.Services))
				for _, service := range req.Services {
					items = append(items, composeApply.Container{
						Service: service,
						Name:    "/" + req.Ref.Name + "-" + service,
						ID:      types.ContainerID("new-" + req.Ref.Name + "-" + service),
						ImageID: types.ImageID("sha256:" + req.Ref.Name),
					})
				}

				return items, nil
			}).
			Twice()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		metrics := gitAssociatedContainer("metrics", "beta-metrics", "beta:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeProjectLabel: "beta",
			composeLabels.ComposeServiceLabel: "metrics",
		})
		dependent := gitAssociatedContainer("dependent", "beta-api", "beta:latest", map[string]string{
			labels.ComposeDir:                           dir,
			composeLabels.ComposeProjectLabel:           "beta",
			composeLabels.ComposeServiceLabel:           "api",
			"com.centurylinklabs.watchtower.depends-on": "alpha-api",
		})
		provider := gitAssociatedContainer("provider", "alpha-api", "alpha:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeProjectLabel: "alpha",
			composeLabels.ComposeServiceLabel: "api",
		})

		metrics.SetStale(true)
		dependent.SetStale(true)
		provider.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "abc"}
		sess.store(metrics.ID(), result)
		sess.store(dependent.ID(), result)
		sess.store(provider.ID(), result)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(
			newTestLogger(t),
			t.Context(),
			docker,
			[]types.Container{metrics, dependent, provider},
			types.UpdateParams{EnableGitMonitoring: true},
			nil,
			failed,
		)

		assert.Empty(t, failed)
		assert.Equal(t, []string{"alpha", "beta"}, order)
	})

	t.Run("keeps a Compose apply that returns success after its timeout elapses", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			RunAndReturn(func(context.Context, composeApply.Request) ([]composeApply.Container, error) {
				time.Sleep(30 * time.Millisecond)

				return []composeApply.Container{{
					Service: "api",
					Name:    "/web-api",
					ID:      "new-api",
					ImageID: "sha256:api",
				}}, nil
			}).
			Once()

		sess := NewSession(git.New(newTestLogger(t), git.Options{Timeout: 5 * time.Millisecond}))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		c := gitAssociatedContainer("api", "web-api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeProjectLabel: "webstack",
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(
			newTestLogger(t),
			t.Context(),
			docker,
			[]types.Container{c},
			types.UpdateParams{EnableGitMonitoring: true},
			nil,
			failed,
		)

		assert.Empty(t, failed)
		assert.Contains(t, sess.applied, c.ID())
	})

	t.Run("does not apply after the parent context is canceled during checkout", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		parent, cancel := context.WithCancel(t.Context())

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(_ context.Context, _, _ string, opts project.CheckoutOptions) error {
			if opts.Prior != nil {
				*opts.Prior = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			}

			cancel()

			return nil
		}

		c := gitAssociatedContainer("api", "web-api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeProjectLabel: "webstack",
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(
			newTestLogger(t),
			parent,
			docker,
			[]types.Container{c},
			types.UpdateParams{EnableGitMonitoring: true},
			nil,
			failed,
		)

		require.ErrorIs(t, failed[c.ID()], context.Canceled)
		assert.Empty(t, sess.applied)
	})

	t.Run("reports a normal Compose apply as updated", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return([]composeApply.Container{{Service: "api", ID: "new-api", ImageID: "sha256:api"}}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		c := gitAssociatedContainer("api", "web-1", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeProjectLabel: "webstack",
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "0123456789abcdef0123456789abcdef01234567", Tag: "v1.2.3"}
		sess.store(c.ID(), result)

		params := types.UpdateParams{EnableGitMonitoring: true}
		progress := &state.Progress{}
		progress.AddScanned(newTestLogger(t), c, types.ImageID("git:"+result.Commit), params)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{c}, params, progress, failed)

		report := progress.Report(newTestLogger(t))

		assert.Empty(t, failed)
		require.Len(t, report.Updated(), 1)
		assert.Equal(t, types.ContainerID("new-api"), report.Updated()[0].NewContainerID())
		assert.Empty(t, report.Stale())
	})

	t.Run("fails a compose container that has no service label", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		sess := NewSession(gitTestClient(t))
		sess.apply = mockRebuild.NewMockApplier(t)
		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir: dir,
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		require.ErrorIs(t, failed[c.ID()], errGitComposeService)
		assert.False(t, c.IsStale())
	})

	t.Run("aborts the project when checkout fails", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return project.ErrDirty
		}

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		require.ErrorIs(t, failed[c.ID()], project.ErrDirty)
		assert.False(t, c.IsStale())
	})

	t.Run("applies after checkout returns nil when only its timeout elapsed", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return([]composeApply.Container{{Service: "api", Name: "/api", ID: "new-api", ImageID: "sha256:api"}}, nil).
			Once()

		sess := NewSession(git.New(newTestLogger(t), git.Options{Timeout: 10 * time.Millisecond}))
		sess.apply = applier
		sess.checkout = func(ctx context.Context, _, _ string, _ project.CheckoutOptions) error {
			<-ctx.Done()

			return nil
		}

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.Empty(t, failed)
		assert.Contains(t, sess.applied, c.ID())
	})

	t.Run("unmarks stale containers when compose apply fails", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return([]composeApply.Container{
				{Service: "api", Name: "/api", ID: "new-api", ImageID: "sha256:api"},
				{Service: "api", Name: "/fresh", ID: "new-fresh", ImageID: "sha256:api"},
			}, context.Canceled).
			Once()

		var logs bytes.Buffer

		log := zerolog.New(&logs)
		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		fresh := gitAssociatedContainer("fresh", "/fresh", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})

		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		params := types.UpdateParams{EnableGitMonitoring: true}
		progress := &state.Progress{}
		progress.AddScanned(&log, c, types.ImageID("git:new"), params)
		progress.AddScanned(&log, fresh, fresh.ImageID(), params)
		sess.PrepareRebuilds(&log, t.Context(), docker, []types.Container{c}, params, progress, failed)
		progress.UpdateFailed(&log, failed)

		report := progress.Report(&log)

		require.ErrorIs(t, failed[c.ID()], context.Canceled)
		assert.Contains(t, failed[c.ID()].Error(), "partially recreated services")
		require.ErrorIs(t, failed[fresh.ID()], context.Canceled)
		assert.False(t, c.IsStale())
		assert.Empty(t, sess.applied)
		assert.Empty(t, report.Updated())
		require.Len(t, report.Failed(), 2)
		assert.Contains(t, logs.String(), "may have partially recreated services")
		assert.NotContains(t, logs.String(), "Leaving running container untouched")
	})

	t.Run("fails a service omitted from the compose apply result", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return([]composeApply.Container{{Service: "api", ID: "new-api", ImageID: "sha256:api"}}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		api := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		worker := gitAssociatedContainer("worker", "/worker", "webstack-worker:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "worker",
		})

		api.SetStale(true)
		worker.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "abc"}
		sess.store(api.ID(), result)
		sess.store(worker.ID(), result)

		var logs bytes.Buffer

		log := zerolog.New(&logs)
		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		params := types.UpdateParams{EnableGitMonitoring: true}
		progress := &state.Progress{}
		progress.AddScanned(&log, api, types.ImageID("git:abc"), params)
		progress.AddScanned(&log, worker, types.ImageID("git:abc"), params)
		sess.PrepareRebuilds(&log, t.Context(), docker, []types.Container{api, worker}, params, progress, failed)
		progress.UpdateFailed(&log, failed)

		report := progress.Report(&log)

		assert.NotContains(t, failed, api.ID())
		require.ErrorIs(t, failed[worker.ID()], errGitComposeInstance)
		assert.False(t, worker.IsStale())
		assert.Contains(t, sess.applied, api.ID())
		assert.NotContains(t, sess.applied, worker.ID())
		require.Len(t, report.Updated(), 1)
		require.Len(t, report.Failed(), 1)
		assert.Contains(t, logs.String(), "Runtime state is unknown")
		assert.NotContains(t, logs.String(), "Leaving running container untouched")
	})

	t.Run("builds only when no-restart is set", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)

		var got composeApply.Request

		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Run(func(_ context.Context, req composeApply.Request) {
				got = req
			}).
			Return([]composeApply.Container{{Service: "api", ImageID: "sha256:built"}}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		params := types.UpdateParams{EnableGitMonitoring: true, NoRestart: true}
		progress := &state.Progress{}
		progress.AddScanned(newTestLogger(t), c, types.ImageID("git:abc"), params)
		sess.PrepareRebuilds(
			newTestLogger(t),
			t.Context(),
			docker,
			[]types.Container{c},
			params,
			progress,
			failed,
		)

		report := progress.Report(newTestLogger(t))

		assert.Empty(t, failed)
		assert.True(t, got.BuildOnly)
		assert.Empty(t, sess.applied)
		assert.Equal(t, types.ImageID("sha256:built"), sess.built[c.ID()])
		assert.True(t, sess.SkipRecreate(c, params))
		assert.Empty(t, report.Updated())
		require.Len(t, report.Stale(), 1)
	})

	t.Run("reports Compose build failures without partial runtime claims", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return(nil, context.Canceled).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		var logs bytes.Buffer

		log := zerolog.New(&logs)
		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		params := types.UpdateParams{EnableGitMonitoring: true, NoRestart: true}
		sess.PrepareRebuilds(&log, t.Context(), docker, []types.Container{c}, params, nil, failed)

		require.ErrorIs(t, failed[c.ID()], context.Canceled)
		assert.Contains(t, failed[c.ID()].Error(), "compose build failed")
		assert.False(t, c.IsStale())
		assert.Empty(t, sess.built)
		assert.Empty(t, sess.applied)
		assert.NotContains(t, logs.String(), "partially recreated services")
	})

	t.Run("fails the project when services resolve different commits", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		checkedOut := false
		sess := NewSession(gitTestClient(t))
		sess.apply = mockRebuild.NewMockApplier(t)
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			checkedOut = true

			return nil
		}

		api := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		worker := gitAssociatedContainer("worker", "/worker", "webstack-worker:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "worker",
		})

		api.SetStale(true)
		worker.SetStale(true)
		sess.store(api.ID(), git.CheckResult{Stale: true, Commit: "aaa"})
		sess.store(worker.ID(), git.CheckResult{Stale: true, Commit: "bbb"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{api, worker}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.False(t, checkedOut)
		require.ErrorIs(t, failed[api.ID()], errGitComposeCommit)
		require.ErrorIs(t, failed[worker.ID()], errGitComposeCommit)
		assert.False(t, api.IsStale())
		assert.False(t, worker.IsStale())
	})

	t.Run("stamps each service from its own tag", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)

		var got composeApply.Request

		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Run(func(_ context.Context, req composeApply.Request) {
				got = req
			}).
			Return([]composeApply.Container{
				{Service: "api", ID: "new-api", ImageID: "sha256:api"},
				{Service: "worker", ID: "new-worker", ImageID: "sha256:worker"},
			}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		api := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		worker := gitAssociatedContainer("worker", "/worker", "webstack-worker:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "worker",
		})

		api.SetStale(true)
		worker.SetStale(true)
		sess.store(api.ID(), git.CheckResult{Stale: true, Commit: "abc", Tag: "v1.2.3"})
		sess.store(worker.ID(), git.CheckResult{Stale: true, Commit: "abc", Tag: "v1.2.4"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{api, worker}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.Empty(t, failed)
		assert.Equal(t, "v1.2.3", got.Labels["api"][labels.GitLastTag])
		assert.Equal(t, "v1.2.4", got.Labels["worker"][labels.GitLastTag])
		assert.Contains(t, sess.applied, api.ID())
		assert.Contains(t, sess.applied, worker.ID())
	})

	t.Run("marks every replica of a service applied", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)

		var got composeApply.Request

		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Run(func(_ context.Context, req composeApply.Request) {
				got = req
			}).
			Return([]composeApply.Container{
				{Service: "api", Name: "/web-1", ID: "new-1", ImageID: "sha256:api"},
				{Service: "api", Name: "/web-2", ID: "new-2", ImageID: "sha256:api"},
			}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		one := gitAssociatedContainer("api-1", "web-1", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		two := gitAssociatedContainer("api-2", "web-2", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})

		one.SetStale(true)
		two.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "abc"}
		sess.store(one.ID(), result)
		sess.store(two.ID(), result)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{one, two}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.Empty(t, failed)
		assert.Equal(t, []string{"api"}, got.Services)
		assert.Contains(t, sess.applied, one.ID())
		assert.Contains(t, sess.applied, two.ID())
	})

	t.Run("tracks every recreated replica in a mixed stale and fresh service", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return([]composeApply.Container{
				{Service: "api", Name: "/web-1", ID: "new-1", ImageID: "sha256:api"},
				{Service: "api", Name: "/web-2", ID: "new-2", ImageID: "sha256:api"},
			}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		stale := gitAssociatedContainer("api-1", "web-1", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		fresh := gitAssociatedContainer("api-2", "web-2", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})

		stale.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "abc", Tag: "v1.2.3"}
		sess.store(stale.ID(), result)

		params := types.UpdateParams{EnableGitMonitoring: true}
		progress := &state.Progress{}
		progress.AddScanned(newTestLogger(t), stale, types.ImageID("git:"+result.Commit), params)
		progress.AddScanned(newTestLogger(t), fresh, fresh.ImageID(), params)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{stale}, params, progress, failed)

		report := progress.Report(newTestLogger(t))

		assert.Empty(t, failed)
		assert.Contains(t, sess.applied, stale.ID())
		assert.Contains(t, sess.applied, fresh.ID())
		require.Len(t, report.Updated(), 2)
		assert.Empty(t, report.Stale())
		assert.NotEmpty(t, report.Updated()[0].NewContainerID())
		assert.NotEmpty(t, report.Updated()[1].NewContainerID())
		assert.Empty(t, sess.ExcludeApplied([]types.Container{stale, fresh}))
	})

	t.Run("accepts additional Compose replicas absent from filtered progress", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return([]composeApply.Container{
				{Service: "api", Name: "/web-1", ID: "new-1", ImageID: "sha256:api"},
				{Service: "api", Name: "/web-2", ID: "new-2", ImageID: "sha256:api"},
			}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		stale := gitAssociatedContainer("api-1", "web-1", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		fresh := gitAssociatedContainer("api-2", "web-2", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})

		stale.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "abc", Tag: "v1.2.3"}
		sess.store(stale.ID(), result)

		params := types.UpdateParams{EnableGitMonitoring: true}
		progress := &state.Progress{}
		progress.AddScanned(newTestLogger(t), stale, types.ImageID("git:"+result.Commit), params)

		var logs bytes.Buffer

		log := zerolog.New(&logs)
		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(&log, t.Context(), docker, []types.Container{stale}, params, progress, failed)

		report := progress.Report(&log)

		assert.Empty(t, failed)
		assert.Contains(t, sess.applied, stale.ID())
		assert.NotContains(t, sess.applied, fresh.ID())
		require.Len(t, report.Updated(), 1)
		assert.Contains(t, logs.String(), "replica absent from filtered progress")
	})

	t.Run("does not apply a replica compose did not identify", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return([]composeApply.Container{
				{Service: "api", Name: "web-1", ID: "new-1", ImageID: "sha256:api"},
			}, nil).
			Once()

		sess := NewSession(gitTestClient(t))
		sess.apply = applier
		sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
			return nil
		}

		one := gitAssociatedContainer("api-1", "web-1", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})
		two := gitAssociatedContainer("api-2", "web-2", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 dir,
			composeLabels.ComposeServiceLabel: "api",
		})

		one.SetStale(true)
		two.SetStale(true)

		result := git.CheckResult{Stale: true, Commit: "abc"}
		sess.store(one.ID(), result)
		sess.store(two.ID(), result)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{one, two}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.NotContains(t, failed, one.ID())
		require.ErrorIs(t, failed[two.ID()], errGitComposeInstance)
		assert.False(t, two.IsStale())
		assert.Contains(t, sess.applied, one.ID())
		assert.NotContains(t, sess.applied, two.ID())
	})
}

// Test_composeServiceLabels verifies the labels written onto Compose services,
// including the redaction of credentials.
func Test_composeServiceLabels(t *testing.T) {
	t.Parallel()

	t.Run("omits git-watch when only process-wide enable is set", func(t *testing.T) {
		t.Parallel()

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", nil)
		got := composeServiceLabels(c, types.UpdateParams{EnableGitMonitoring: true}, git.CheckResult{Commit: "abc"})
		assert.NotContains(t, got, labels.GitWatch)
		assert.Equal(t, "https://github.com/org/app.git", got[labels.GitRepo])
	})

	t.Run("redacts credentials from the Compose repository label", func(t *testing.T) {
		t.Parallel()

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.GitRepo: "https://user:secret@example.com/org/app.git?token=secret",
		})
		got := composeServiceLabels(c, types.UpdateParams{EnableGitMonitoring: true}, git.CheckResult{Commit: "abc"})
		assert.Equal(t, "https://example.com/org/app.git", got[labels.GitRepo])
		assert.NotContains(t, got[labels.GitRepo], "secret")
	})

	t.Run("redacts API origin credentials while preserving path and port", func(t *testing.T) {
		t.Parallel()

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.GitHost: "https://user:secret@git.example.com:8443/git/api?token=secret",
		})
		got := composeServiceLabels(c, types.UpdateParams{EnableGitMonitoring: true}, git.CheckResult{Commit: "abc"})
		assert.Equal(t, "https://git.example.com:8443/git/api", got[labels.GitHost])
		assert.NotContains(t, got[labels.GitHost], "secret")
	})

	t.Run("omits malformed API origins", func(t *testing.T) {
		t.Parallel()

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.GitHost: "https://",
		})
		got := composeServiceLabels(c, types.UpdateParams{EnableGitMonitoring: true}, git.CheckResult{Commit: "abc"})
		assert.NotContains(t, got, labels.GitHost)
	})

	t.Run("writes git-watch when the source label enabled it", func(t *testing.T) {
		t.Parallel()

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.GitWatch: "true",
		})
		got := composeServiceLabels(c, types.UpdateParams{}, git.CheckResult{Commit: "abc"})
		assert.Equal(t, "true", got[labels.GitWatch])
	})
}
