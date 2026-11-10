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

	composeApply "github.com/nicholas-fedor/watchtower/internal/compose/apply"
	composeLabels "github.com/nicholas-fedor/watchtower/internal/compose/labels"
	composeProject "github.com/nicholas-fedor/watchtower/internal/compose/project"
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/internal/git/project"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	updateConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/update"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	mockRebuild "github.com/nicholas-fedor/watchtower/internal/update/rebuild/mocks"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
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
func expectGitBuild(client *mockRebuild.MockImageBuilder) {
	client.EXPECT().BuildRemoteImage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, _, _ string, tags []string) (container.ImageID, error) {
			err := ctx.Err()
			if err != nil {
				return "", err
			}

			if len(tags) > 0 {
				return container.ImageID("sha256:gitbuild" + tags[0]), nil
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
//   - *container.Container: The configured container.
func gitAssociatedContainer(id, name, image string, extra map[string]string) *container.Container {
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

		assert.False(t, sess.Watching(newTestLogger(t), c, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}))
	})

	t.Run("is false when the client is nil", func(t *testing.T) {
		t.Parallel()

		sess := &Session{}
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

		assert.False(t, sess.Watching(newTestLogger(t), c, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}))
	})

	t.Run("is true for an associated container when monitoring is enabled", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)

		assert.True(t, sess.Watching(newTestLogger(t), c, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}))
	})

	t.Run("is false when git-watch is false", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", map[string]string{
			labels.GitWatch: "false",
		})

		assert.False(t, sess.Watching(newTestLogger(t), c, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}))
	})

	t.Run("is false for an unassociated container", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := mockUpdate.CreateMockContainer("id", "/app", "myapp:latest", time.Now())

		assert.False(t, sess.Watching(newTestLogger(t), c, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}))
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

		assert.False(t, sess.Watching(newTestLogger(t), c, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}))
	})
}

// TestSession_PrepareRebuilds verifies which stale Git-associated containers are
// rebuilt, and how a failed build is recorded. It also checks that a container
// whose compose-dir is not a Compose project is skipped rather than rebuilt.
func TestSession_PrepareRebuilds(t *testing.T) {
	t.Parallel()

	t.Run("returns immediately when the session or client is nil", func(t *testing.T) {
		t.Parallel()

		failed := map[container.ContainerID]error{}
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		c.SetStale(true)

		var sess *Session
		sess.PrepareRebuilds(
			newTestLogger(t),
			t.Context(),
			mockRebuild.NewMockImageBuilder(t),
			[]*container.Container{c},
			updateParams.Params{},
			nil,
			failed,
		)
		assert.Empty(t, failed)

		sess = &Session{results: map[container.ContainerID]git.CheckResult{}}
		sess.PrepareRebuilds(
			newTestLogger(t),
			t.Context(),
			mockRebuild.NewMockImageBuilder(t),
			[]*container.Container{c},
			updateParams.Params{},
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

		failed := map[container.ContainerID]error{}
		// The mock has no expectations, so a build or image removal fails
		// the test.
		docker := mockRebuild.NewMockImageBuilder(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []*container.Container{fresh, staleNoResult, staleFreshResult}, updateParams.Params{}, nil, failed)

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

		failed := map[container.ContainerID]error{}
		docker := mockRebuild.NewMockImageBuilder(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []*container.Container{monitor, noPull}, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}, nil, failed)

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

		failed := map[container.ContainerID]error{}
		docker := mockRebuild.NewMockImageBuilder(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []*container.Container{c}, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}, nil, failed)

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

		failed := map[container.ContainerID]error{}
		docker := mockRebuild.NewMockImageBuilder(t)
		expectGitBuild(docker)

		progress := &state.Progress{}
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []*container.Container{c}, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}, progress, failed)

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

		failed := map[container.ContainerID]error{}
		docker := mockRebuild.NewMockImageBuilder(t)
		sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []*container.Container{c}, updateParams.Params{Git: gitConfig.Defaults{Enable: true}}, nil, failed)

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

		params := updateParams.Params{Git: gitConfig.Defaults{Enable: true}}
		log := newTestLogger(t)
		progress := &state.Progress{}
		progress.AddScanned(log, c, container.ImageID("git:abc"), params)

		failed := map[container.ContainerID]error{}
		docker := mockRebuild.NewMockImageBuilder(t)
		sess.PrepareRebuilds(log, t.Context(), docker, []*container.Container{c}, params, progress, failed)
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

// TestSession_ExcludeApplied verifies that containers Compose apply already
// recreated are left out of recreation.
func TestSession_ExcludeApplied(t *testing.T) {
	t.Parallel()

	t.Run("returns the original list when the session is nil or nothing was applied", func(t *testing.T) {
		t.Parallel()

		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		list := []*container.Container{c}

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

		got := sess.ExcludeApplied([]*container.Container{applied, kept})
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
		assert.Nil(t, sess.NoRestartBuiltIDs(updateParams.Params{NoRestart: true}))

		sess = NewSession(gitTestClient(t))
		sess.built["built"] = "sha256:git"
		assert.Nil(t, sess.NoRestartBuiltIDs(updateParams.Params{}))
	})

	t.Run("returns a copy of the built container IDs during a no-restart update", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		sess.built["built"] = "sha256:git"

		got := sess.NoRestartBuiltIDs(updateParams.Params{NoRestart: true})
		assert.Equal(t, map[container.ContainerID]struct{}{"built": {}}, got)

		got["other"] = struct{}{}

		assert.Len(t, sess.built, 1, "the snapshot must not share the session's map")
	})
}

// TestSession_reuseUnrestartedBuild covers repeated sessions with the
// no-restart configuration option. The running container keeps its old stamp,
// so every session's check is stale for the same commit. Only the first
// session builds. Later sessions reuse its image until the remote advances.
func TestSession_reuseUnrestartedBuild(t *testing.T) {
	t.Parallel()

	const (
		commit = "0123456789abcdef0123456789abcdef01234567"
		newer  = "fedcba9876543210fedcba9876543210fedcba98"
	)

	// runSession runs one update session's rebuild step for a freshly listed
	// container, as each scheduled run does, sharing the process's Git client.
	runSession := func(
		t *testing.T,
		client *git.Client,
		docker ImageBuilder,
		result git.CheckResult,
		params updateParams.Params,
	) (*Session, *container.Container, *state.Report, map[container.ContainerID]error) {
		t.Helper()

		sess := NewSession(client)
		sess.apply = mockRebuild.NewMockApplier(t)

		c := gitAssociatedContainer("app", "/app", "myapp:latest", nil)
		c.SetStale(true)
		sess.store(c.ID(), result)

		log := newTestLogger(t)
		progress := &state.Progress{}
		progress.AddScanned(log, c, container.ImageID("git:"+result.Commit), params)

		failed := map[container.ContainerID]error{}
		sess.PrepareRebuilds(log, t.Context(), docker, []*container.Container{c}, params, progress, failed)

		return sess, c, progress.Report(log), failed
	}

	noRestart := updateParams.Params{NoRestart: true, Git: gitConfig.Defaults{Enable: true}}
	stale := git.CheckResult{Stale: true, Commit: commit, Tag: "v1.0.0"}

	t.Run("builds once and reuses the image in later sessions", func(t *testing.T) {
		t.Parallel()

		client := gitTestClient(t)
		docker := mockRebuild.NewMockImageBuilder(t)
		expectGitBuild(docker)

		first, _, _, failed := runSession(t, client, docker, stale, noRestart)
		require.Empty(t, failed)

		built := first.built["app"]
		require.NotEmpty(t, built)
		assert.Equal(t, built, client.UnrestartedBuild("app", commit))

		// The mock allows one build. A second build fails the test.
		second, c, report, failed := runSession(t, client, docker, stale, noRestart)

		assert.Empty(t, failed)
		assert.Equal(t, built, second.built[c.ID()])
		assert.Contains(t, second.NoRestartBuiltIDs(noRestart), c.ID(), "the container stays out of the restart phase")
		require.Len(t, report.Stale(), 1)
		assert.Equal(t, built, report.Stale()[0].LatestImageID())
		assert.Equal(t, "https://github.com/org/app/releases/tag/v1.0.0", report.Stale()[0].Changelog())
	})

	t.Run("runs one Compose build and reuses it in later sessions", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeProjectDir(t)
		client := gitTestClient(t)
		applier := mockRebuild.NewMockApplier(t)
		applier.EXPECT().
			Apply(mock.Anything, mock.Anything).
			Return([]composeApply.Container{{Service: "api", ImageID: "sha256:built"}}, nil).
			Once()

		// Each session lists the container fresh. The applier allows one build.
		for range 2 {
			sess := NewSession(client)
			sess.apply = applier
			sess.checkout = func(context.Context, string, string, project.CheckoutOptions) error {
				return nil
			}

			c := gitAssociatedContainer("api", "/api", "webstack-api:latest", map[string]string{
				labels.ComposeDir:                 dir,
				composeLabels.ComposeServiceLabel: "api",
			})
			c.SetStale(true)
			sess.store(c.ID(), stale)

			failed := map[container.ContainerID]error{}
			docker := mockRebuild.NewMockImageBuilder(t)
			sess.PrepareRebuilds(newTestLogger(t), t.Context(), docker, []*container.Container{c}, noRestart, nil, failed)

			assert.Empty(t, failed)
			assert.Equal(t, container.ImageID("sha256:built"), sess.built[c.ID()])
			assert.Contains(t, sess.NoRestartBuiltIDs(noRestart), c.ID())
		}
	})

	t.Run("builds again once the remote advances", func(t *testing.T) {
		t.Parallel()

		client := gitTestClient(t)
		client.RecordUnrestartedBuild("app", commit, "sha256:older")

		docker := mockRebuild.NewMockImageBuilder(t)
		expectGitBuild(docker)

		sess, c, _, failed := runSession(t, client, docker, git.CheckResult{Stale: true, Commit: newer}, noRestart)

		assert.Empty(t, failed)
		assert.NotEqual(t, container.ImageID("sha256:older"), sess.built[c.ID()])
		assert.Equal(t, sess.built[c.ID()], client.UnrestartedBuild("app", newer))
	})

	// With restarts on, the container is recreated from the new image, so an
	// earlier record must not stop the build and nothing new is recorded.
	t.Run("builds and records nothing when restarts are enabled", func(t *testing.T) {
		t.Parallel()

		client := gitTestClient(t)
		client.RecordUnrestartedBuild("app", commit, "sha256:older")

		docker := mockRebuild.NewMockImageBuilder(t)
		expectGitBuild(docker)

		sess, c, _, failed := runSession(t, client, docker, stale, updateParams.Params{Git: gitConfig.Defaults{Enable: true}})

		assert.Empty(t, failed)
		assert.NotEqual(t, container.ImageID("sha256:older"), sess.built[c.ID()])
		assert.Equal(t, container.ImageID("sha256:older"), client.UnrestartedBuild("app", commit))
	})
}

// TestAllowed verifies that a Git-watched container may be rebuilt unless
// no-pull or monitor-only applies, by label or by policy.
func TestAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		policy updateConfig.Policy
		want   bool
	}{
		{name: "neither applies", want: true},
		{name: "no-pull label", labels: map[string]string{labels.NoPull: "true"}, want: false},
		{name: "no-pull policy", policy: updateConfig.Policy{NoPull: true}, want: false},
		{name: "monitor-only label", labels: map[string]string{labels.MonitorOnly: "true"}, want: false},
		{name: "monitor-only policy", policy: updateConfig.Policy{MonitorOnly: true}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := gitAssociatedContainer("app", "/app", "myapp:latest", tt.labels)

			assert.Equal(t, tt.want, Allowed(c, tt.policy))
		})
	}
}
