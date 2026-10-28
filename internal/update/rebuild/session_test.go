package rebuild

import (
	"context"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	composeLabels "github.com/nicholas-fedor/watchtower/internal/compose/labels"
	composeProject "github.com/nicholas-fedor/watchtower/internal/compose/project"
	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/git"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	mockRebuild "github.com/nicholas-fedor/watchtower/internal/update/rebuild/mocks"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// gitTestClient returns a Git client with a one-second timeout that logs to t.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *git.Client: The client.
func gitTestClient(t *testing.T) *git.Client {
	t.Helper()

	return git.New(newTestLogger(t), git.Options{Timeout: time.Second})
}

// expectGitBuild expects one remote Git build and answers it with an image ID
// derived from the first tag, or with the context's error once it is done.
//
// Parameters:
//   - client: Mocked container client.
func expectGitBuild(client *mockContainerClient.MockClient) {
	client.EXPECT().BuildRemoteImage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, _, _ string, tags []string) (types.ImageID, error) {
			err := ctx.Err()
			if err != nil {
				return "", err
			}

			if len(tags) > 0 {
				return types.ImageID("sha256:gitbuild" + tags[0]), nil
			}

			return "sha256:gitbuild", nil
		},
	).Once()
}

// gitAssociatedContainer returns a running container associated with the
// Git repository https://github.com/org/app.git on its main branch.
//
// Parameters:
//   - id: Container ID.
//   - name: Container name.
//   - image: Image reference the container runs.
//   - extra: Labels added to the Git association labels.
//
// Returns:
//   - types.Container: The configured container.
func gitAssociatedContainer(id, name, image string, extra map[string]string) types.Container {
	labels := map[string]string{
		labels.GitRepo: "https://github.com/org/app.git",
		labels.GitRef:  "main",
	}

	maps.Copy(labels, extra)

	return mockUpdate.CreateMockContainerWithConfig(
		id,
		name,
		image,
		true,
		false,
		time.Now().Add(-time.Hour),
		&dockerContainer.Config{
			Image:  image,
			Labels: labels,
		},
	)
}

// TestNewSession verifies that a new session starts with empty result and built
// maps.
func TestNewSession(t *testing.T) {
	t.Parallel()

	t.Run("returns empty result and built maps", func(t *testing.T) {
		t.Parallel()

		client := gitTestClient(t)
		sess := NewSession(client)

		require.NotNil(t, sess)
		assert.Equal(t, client, sess.client)
		assert.Empty(t, sess.results)
		assert.Empty(t, sess.built)
	})
}

// TestSession_storeAndResult verifies that a stored Git check result is returned
// for its container.
func TestSession_storeAndResult(t *testing.T) {
	t.Parallel()

	t.Run("does nothing when the session is nil", func(t *testing.T) {
		t.Parallel()

		var sess *Session

		sess.store("abc", git.CheckResult{Stale: true, Commit: "deadbeef"})
		got, ok := sess.result("abc")
		assert.False(t, ok)
		assert.Equal(t, git.CheckResult{}, got)
	})

	t.Run("stores and retrieves a check result", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		want := git.CheckResult{Stale: true, Commit: "abc123", Tag: "v1.2.3", Kind: "tag", Ref: "v1.2.3"}

		sess.store("c1", want)
		got, ok := sess.result("c1")
		assert.True(t, ok)
		assert.Equal(t, want, got)
	})

	t.Run("returns false for an unknown container", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		_, ok := sess.result("missing")
		assert.False(t, ok)
	})
}

// TestSession_Watching verifies which containers Git monitoring applies to.
func TestSession_Watching(t *testing.T) {
	t.Parallel()

	t.Run("is false when the session is nil", func(t *testing.T) {
		t.Parallel()

		var sess *Session

		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

		assert.False(t, sess.Watching(newTestLogger(t), c, types.UpdateParams{EnableGitMonitoring: true}))
	})

	t.Run("is false when the client is nil", func(t *testing.T) {
		t.Parallel()

		sess := &Session{}
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

		assert.False(t, sess.Watching(newTestLogger(t), c, types.UpdateParams{EnableGitMonitoring: true}))
	})

	t.Run("is true for an associated container when monitoring is enabled", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

		assert.True(t, sess.Watching(newTestLogger(t), c, types.UpdateParams{EnableGitMonitoring: true}))
	})

	t.Run("is false when git-watch is false", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", map[string]string{
			labels.GitWatch: "false",
		})

		assert.False(t, sess.Watching(newTestLogger(t), c, types.UpdateParams{EnableGitMonitoring: true}))
	})

	t.Run("is false for an unassociated container", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := mockUpdate.CreateMockContainer("id", "/app", "myapp:latest", time.Now())

		assert.False(t, sess.Watching(newTestLogger(t), c, types.UpdateParams{EnableGitMonitoring: true}))
	})

	t.Run("is false for Watchtower itself", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := mockUpdate.CreateMockContainerWithConfig(
			"wt",
			"/watchtower",
			"nickfedor/watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Image: "nickfedor/watchtower:latest",
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
					labels.GitRepo:                   "https://github.com/nicholas-fedor/watchtower.git",
					labels.GitWatch:                  "true",
				},
			},
		)

		assert.False(t, sess.Watching(newTestLogger(t), c, types.UpdateParams{EnableGitMonitoring: true}))
	})
}

// TestSession_PrepareRebuilds verifies which stale Git-associated containers are
// rebuilt, and how a failed build is recorded. It also checks that a container
// whose compose-dir is not a Compose project is skipped rather than rebuilt.
func TestSession_PrepareRebuilds(t *testing.T) {
	t.Parallel()

	t.Run("returns immediately when the session or client is nil", func(t *testing.T) {
		t.Parallel()

		failed := map[types.ContainerID]error{}
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		c.SetStale(true)

		var sess *Session
		sess.PrepareRebuilds(
			newTestLogger(t),
			t.Context(),
			mockContainerClient.NewMockClient(t),
			[]types.Container{c},
			types.UpdateParams{},
			nil,
			failed,
		)
		assert.Empty(t, failed)

		sess = &Session{results: map[types.ContainerID]git.CheckResult{}}
		sess.PrepareRebuilds(
			newTestLogger(t),
			t.Context(),
			mockContainerClient.NewMockClient(t),
			[]types.Container{c},
			types.UpdateParams{},
			nil,
			failed,
		)
		assert.Empty(t, failed)
	})

	t.Run("skips containers that are not stale or have no stored stale result", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		sess.apply = mockRebuild.NewMockApplier(t)
		fresh := gitAssociatedContainer("fresh", "/fresh", "myapp:latest", nil)
		staleNoResult := gitAssociatedContainer("none", "/none", "myapp:latest", nil)
		staleFreshResult := gitAssociatedContainer("stored", "/stored", "myapp:latest", nil)

		staleNoResult.SetStale(true)
		staleFreshResult.SetStale(true)
		sess.store(staleFreshResult.ID(), git.CheckResult{Stale: false})

		failed := map[types.ContainerID]error{}
		// The mock has no expectations, so a build or image removal fails
		// the test.
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{fresh, staleNoResult, staleFreshResult}, types.UpdateParams{}, nil, failed)

		assert.Empty(t, failed)
	})

	t.Run("skips monitor-only and no-pull containers", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		sess.apply = mockRebuild.NewMockApplier(t)
		monitor := gitAssociatedContainer("mon", "/mon", "myapp:latest", map[string]string{
			"com.centurylinklabs.watchtower.monitor-only": "true",
		})
		noPull := gitAssociatedContainer("np", "/np", "myapp:latest", map[string]string{
			"com.centurylinklabs.watchtower.no-pull": "true",
		})

		monitor.SetStale(true)
		noPull.SetStale(true)
		sess.store(monitor.ID(), git.CheckResult{Stale: true, Commit: "abc123abc123"})
		sess.store(noPull.ID(), git.CheckResult{Stale: true, Commit: "abc123abc123"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{monitor, noPull}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.Empty(t, failed)
		assert.Empty(t, sess.built)
	})

	t.Run("records a failed build and unmarks the container stale", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		sess.apply = mockRebuild.NewMockApplier(t)
		c := gitAssociatedContainer("fail", "/fail", "myapp:latest", nil)
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: ""})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.Contains(t, failed, c.ID())
		assert.False(t, c.IsStale())
	})

	t.Run("builds a stale associated container", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		sess.apply = mockRebuild.NewMockApplier(t)
		c := gitAssociatedContainer("ok", "/ok", "myapp:latest", nil)
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "0123456789abcdef0123456789abcdef01234567", Tag: "v1.0.0"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		expectGitBuild(docker)

		progress := &state.Progress{}
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, progress, failed)

		assert.Empty(t, failed)
		assert.Contains(t, sess.built, c.ID())
		assert.True(t, strings.HasPrefix(c.ImageName(), "docker.io/library/myapp:git-"))
		commit, tag := provenance.LastStamp(c)
		assert.Equal(t, "0123456789abcdef0123456789abcdef01234567", commit)
		assert.Equal(t, "v1.0.0", tag)
		assert.Empty(t, sess.applied)
	})

	t.Run("skips when compose-dir is set but the path is not a compose project", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "missing")
		applier := mockRebuild.NewMockApplier(t)
		sess := NewSession(gitTestClient(t))
		sess.apply = applier

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 missing,
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []types.Container{c}, types.UpdateParams{EnableGitMonitoring: true}, nil, failed)

		assert.NotContains(t, failed, c.ID())
		assert.False(t, c.IsStale())
		assert.Empty(t, sess.built)
		assert.Empty(t, sess.applied)
	})

	t.Run("skips a scanned container when compose-dir is set but the path is not a compose project", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "missing")
		applier := mockRebuild.NewMockApplier(t)
		sess := NewSession(gitTestClient(t))
		sess.apply = applier

		c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
			labels.ComposeDir:                 missing,
			composeLabels.ComposeServiceLabel: "api",
		})
		c.SetStale(true)
		sess.store(c.ID(), git.CheckResult{Stale: true, Commit: "abc"})

		params := types.UpdateParams{EnableGitMonitoring: true}
		log := newTestLogger(t)
		progress := &state.Progress{}
		progress.AddScanned(log, c, types.ImageID("git:abc"), params)

		failed := map[types.ContainerID]error{}
		docker := mockContainerClient.NewMockClient(t)
		sess.PrepareRebuilds(log, t.Context(), docker, []types.Container{c}, params, progress, failed)
		progress.UpdateFailed(log, failed)

		report := progress.Report(log)

		assert.NotContains(t, failed, c.ID())
		assert.False(t, c.IsStale())
		assert.Empty(t, sess.built)
		assert.Empty(t, sess.applied)
		require.Len(t, report.Skipped(), 1)
		assert.Equal(t, c.ID(), report.Skipped()[0].ID())
		assert.Contains(t, report.Skipped()[0].Error(), composeProject.ErrProjectDir.Error())
		assert.Empty(t, report.Failed())
		assert.Empty(t, report.Scanned())
	})
}

// TestSession_SkipRecreate verifies when recreating a container from its inspect
// data is skipped after a Git rebuild.
func TestSession_SkipRecreate(t *testing.T) {
	t.Parallel()

	t.Run("is false when the session is nil", func(t *testing.T) {
		t.Parallel()

		var sess *Session

		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

		assert.False(t, sess.SkipRecreate(c, types.UpdateParams{NoRestart: true}))
	})

	t.Run("is false when no-restart is off", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		sess.built[c.ID()] = "sha256:git"

		assert.False(t, sess.SkipRecreate(c, types.UpdateParams{}))
	})

	t.Run("is false for Watchtower even when an image was built", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := mockUpdate.CreateMockContainerWithConfig(
			"wt",
			"/watchtower",
			"nickfedor/watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Image: "nickfedor/watchtower:latest",
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)
		sess.built[c.ID()] = "sha256:git"

		assert.False(t, sess.SkipRecreate(c, types.UpdateParams{NoRestart: true}))
	})

	t.Run("is true when no-restart is on and this session built the image", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		sess.built[c.ID()] = "sha256:git"

		assert.True(t, sess.SkipRecreate(c, types.UpdateParams{NoRestart: true}))
	})
}

// TestSession_ExcludeNoRestart verifies that containers built during a
// no-restart update are left out of recreation.
func TestSession_ExcludeNoRestart(t *testing.T) {
	t.Parallel()

	t.Run("returns the original list when the session is nil or no-restart is off", func(t *testing.T) {
		t.Parallel()

		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		list := []types.Container{c}

		var sess *Session
		assert.Equal(t, list, sess.ExcludeNoRestart(list, types.UpdateParams{NoRestart: true}))

		sess = NewSession(gitTestClient(t))
		assert.Equal(t, list, sess.ExcludeNoRestart(list, types.UpdateParams{}))
	})

	t.Run("drops containers this session built when no-restart is on", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		built := gitAssociatedContainer("built", "/built", "myapp:latest", nil)
		kept := gitAssociatedContainer("kept", "/kept", "other:latest", nil)
		sess.built[built.ID()] = "sha256:git"

		got := sess.ExcludeNoRestart([]types.Container{built, kept}, types.UpdateParams{NoRestart: true})
		require.Len(t, got, 1)
		assert.Equal(t, kept.ID(), got[0].ID())
	})
}

// TestSession_ExcludeApplied verifies that containers Compose apply already
// recreated are left out of recreation.
func TestSession_ExcludeApplied(t *testing.T) {
	t.Parallel()

	t.Run("returns the original list when the session is nil or nothing was applied", func(t *testing.T) {
		t.Parallel()

		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		list := []types.Container{c}

		var sess *Session
		assert.Equal(t, list, sess.ExcludeApplied(list))

		sess = NewSession(gitTestClient(t))
		assert.Equal(t, list, sess.ExcludeApplied(list))
	})

	t.Run("drops compose-applied containers", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		applied := gitAssociatedContainer("applied", "/applied", "myapp:latest", nil)
		kept := gitAssociatedContainer("kept", "/kept", "other:latest", nil)
		sess.applied[applied.ID()] = struct{}{}

		got := sess.ExcludeApplied([]types.Container{applied, kept})
		require.Len(t, got, 1)
		assert.Equal(t, kept.ID(), got[0].ID())
	})
}

// TestSession_NoRestartBuiltIDs verifies the snapshot of containers built
// during a no-restart update.
func TestSession_NoRestartBuiltIDs(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for a nil session or when restarts are enabled", func(t *testing.T) {
		t.Parallel()

		var sess *Session
		assert.Nil(t, sess.NoRestartBuiltIDs(types.UpdateParams{NoRestart: true}))

		sess = NewSession(gitTestClient(t))
		sess.built["built"] = "sha256:git"
		assert.Nil(t, sess.NoRestartBuiltIDs(types.UpdateParams{}))
	})

	t.Run("returns a copy of the built container IDs during a no-restart update", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		sess.built["built"] = "sha256:git"

		got := sess.NoRestartBuiltIDs(types.UpdateParams{NoRestart: true})
		assert.Equal(t, map[types.ContainerID]struct{}{"built": {}}, got)

		got["other"] = struct{}{}

		assert.Len(t, sess.built, 1, "the snapshot must not share the session's map")
	})
}
