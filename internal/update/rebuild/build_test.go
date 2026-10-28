package rebuild

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/git"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestSession_build verifies a remote Git URL build of one container. It checks
// the errors the build returns and the options it passes to Docker. It also
// checks the labels and changelog entry it records.
func TestSession_build(t *testing.T) {
	t.Parallel()

	t.Run("returns errGitNotAssociated when the container has no repo", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := mockUpdate.CreateMockContainer("plain", "/plain", "nginx:latest", time.Now())
		docker := mockContainerClient.NewMockClient(t)

		err := sess.build(newTestLogger(t), t.Context(), docker, c, git.CheckResult{Commit: "abc"}, types.UpdateParams{}, nil)
		require.ErrorIs(t, err, errGitNotAssociated)
	})

	t.Run("returns an error when the commit is empty", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		docker := mockContainerClient.NewMockClient(t)

		err := sess.build(newTestLogger(t), t.Context(), docker, c, git.CheckResult{Stale: true}, types.UpdateParams{EnableGitMonitoring: true}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git remote context:")
	})

	t.Run("returns an error when the build context is cancelled", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		docker := mockContainerClient.NewMockClient(t)
		expectGitBuild(docker)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := sess.build(newTestLogger(t), ctx, docker, c, git.CheckResult{Stale: true, Commit: "abc123abc123"}, types.UpdateParams{EnableGitMonitoring: true}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "build git image:")
	})

	t.Run("logs exactly one changelog entry resolved from the git tag", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", nil)
		docker := mockContainerClient.NewMockClient(t)
		expectGitBuild(docker)

		params := types.UpdateParams{EnableGitMonitoring: true, EnableChangelog: true}
		progress := state.Progress{}
		progress.AddScanned(newTestLogger(t), c, c.ImageID(), params)
		progress.MarkForUpdate(newTestLogger(t), c.ID())

		var buf bytes.Buffer

		log := zerolog.New(&buf)

		err := sess.build(
			&log,
			t.Context(),
			docker,
			c,
			git.CheckResult{Stale: true, Commit: "0123456789abcdef0123456789abcdef01234567", Tag: "v1.2.3"},
			params,
			&progress,
		)
		require.NoError(t, err)

		assert.Equal(t, 1, strings.Count(buf.String(), `"message":"Changelog"`), buf.String())
		assert.Contains(t, buf.String(), `"changelog":"https://github.com/org/app/releases/tag/v1.2.3"`)
		assert.Equal(t, "https://github.com/org/app/releases/tag/v1.2.3", progress[c.ID()].Changelog())
	})

	t.Run("keeps a nested dockerfile and context on the rebuilt container", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", map[string]string{
			labels.GitDockerfile: "build/docker/Dockerfile",
			labels.GitContext:    ".",
		})
		docker := mockContainerClient.NewMockClient(t)
		expectGitBuild(docker)

		err := sess.build(
			newTestLogger(t),
			t.Context(),
			docker,
			c,
			git.CheckResult{Stale: true, Commit: "ffffffffffffffffffffffffffffffffffffffff"},
			types.UpdateParams{EnableGitMonitoring: true},
			nil,
		)
		require.NoError(t, err)

		assoc, ok := provenance.ResolveAssociation(c, types.UpdateParams{EnableGitMonitoring: true})
		assert.True(t, ok)
		assert.Equal(t, "build/docker/Dockerfile", assoc.Dockerfile)
		assert.Equal(t, ".", assoc.Context)

		_, watchSet := c.GetLabel(labels.GitWatch)
		assert.False(t, watchSet)
	})

	t.Run("persists git-watch only when the source label enabled it", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := gitAssociatedContainer("id", "/app", "myapp:latest", map[string]string{
			labels.GitWatch: "yes",
		})
		docker := mockContainerClient.NewMockClient(t)
		expectGitBuild(docker)

		err := sess.build(
			newTestLogger(t),
			t.Context(),
			docker,
			c,
			git.CheckResult{Stale: true, Commit: "ffffffffffffffffffffffffffffffffffffffff"},
			types.UpdateParams{},
			nil,
		)
		require.NoError(t, err)

		got, ok := c.GetLabel(labels.GitWatch)
		assert.True(t, ok)
		assert.Equal(t, "true", got)
	})
	t.Run("does not pass a digest-pinned image as a build tag", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		image := "ghcr.io/org/app@sha256:" + strings.Repeat("a", 64)
		commit := "0123456789abcdef0123456789abcdef01234567"
		c := gitAssociatedContainer("digest", "/digest", image, nil)

		var tags []string

		docker := mockContainerClient.NewMockClient(t)
		docker.EXPECT().
			BuildRemoteImage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Run(func(_ context.Context, _, _ string, got []string) {
				tags = append([]string(nil), got...)
			}).
			Return(types.ImageID("sha256:built"), nil)

		err := sess.build(
			newTestLogger(t),
			t.Context(),
			docker,
			c,
			git.CheckResult{Stale: true, Commit: commit},
			types.UpdateParams{EnableGitMonitoring: true},
			nil,
		)
		require.NoError(t, err)
		require.Len(t, tags, 1)
		assert.Equal(t, "ghcr.io/org/app:git-0123456789ab", tags[0])
		assert.NotContains(t, tags[0], "@")
	})

	t.Run("applies the Git timeout to the URL-context build", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(git.New(newTestLogger(t), git.Options{Timeout: 50 * time.Millisecond}))
		commit := "0123456789abcdef0123456789abcdef01234567"
		c := gitAssociatedContainer("timeout", "/timeout", "myapp:latest", nil)

		docker := mockContainerClient.NewMockClient(t)
		docker.EXPECT().
			BuildRemoteImage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			Run(func(ctx context.Context, _, _ string, _ []string) {
				deadline, ok := ctx.Deadline()
				assert.True(t, ok)
				assert.LessOrEqual(t, time.Until(deadline), 50*time.Millisecond)
			}).
			Return(types.ImageID("sha256:built"), nil)

		err := sess.build(
			newTestLogger(t),
			t.Context(),
			docker,
			c,
			git.CheckResult{Stale: true, Commit: commit},
			types.UpdateParams{EnableGitMonitoring: true},
			nil,
		)
		require.NoError(t, err)
	})

	t.Run("keeps a build that returns nil after its timeout elapses", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(git.New(newTestLogger(t), git.Options{Timeout: 10 * time.Millisecond}))
		c := gitAssociatedContainer("timeout", "/timeout", "myapp:latest", nil)
		commit := "0123456789abcdef0123456789abcdef01234567"

		docker := mockContainerClient.NewMockClient(t)
		docker.EXPECT().
			BuildRemoteImage(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(ctx context.Context, _, _ string, _ []string) (types.ImageID, error) {
				<-ctx.Done()

				return "sha256:built", nil
			}).
			Once()

		err := sess.build(
			newTestLogger(t),
			t.Context(),
			docker,
			c,
			git.CheckResult{Stale: true, Commit: commit},
			types.UpdateParams{EnableGitMonitoring: true},
			nil,
		)

		require.NoError(t, err)
		assert.Equal(t, types.ImageID("sha256:built"), sess.built[c.ID()])
	})
}

// Test_gitImageTag verifies the name:git-<shortsha> tag of a Git build.
func Test_gitImageTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		image  string
		commit string
		want   string
	}{
		{name: "docker official image", image: "nginx:latest", commit: "0123456789abcdef0123456789abcdef01234567", want: "docker.io/library/nginx:git-0123456789ab"},
		{name: "already normalized name", image: "docker.io/library/myapp:latest", commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", want: "docker.io/library/myapp:git-aaaaaaaaaaaa"},
		{name: "short commit is kept", image: "myapp:latest", commit: "abc", want: "docker.io/library/myapp:git-abc"},
		{name: "empty commit", image: "myapp:latest", commit: "", want: "docker.io/library/myapp:git-"},
		{name: "unparseable name strips a tag suffix", image: "not a ref:dev", commit: "0123456789abcdef", want: "not a ref:git-0123456789ab"},
		{name: "registry with port is not stripped at the port colon", image: "localhost:5000/app", commit: "0123456789abcdef0123", want: "localhost:5000/app:git-0123456789ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			image, commit, want := tt.image, tt.commit, tt.want

			assert.Equal(t, want, gitImageTag(image, commit))
		})
	}
}

// Test_validDockerTag verifies which image names a Git build may also tag, so a
// digest-pinned or unparseable name is never passed to Docker as a tag.
func Test_validDockerTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		image string
		want  bool
	}{
		{name: "tagged image", image: "myapp:latest", want: true},
		{name: "bare image", image: "myapp", want: true},
		{name: "digest image", image: "myapp@sha256:" + strings.Repeat("a", 64), want: false},
		{name: "tagged digest image", image: "myapp:latest@sha256:" + strings.Repeat("a", 64), want: false},
		{name: "invalid image", image: "not a ref:dev", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			image, want := tt.image, tt.want

			assert.Equal(t, want, validDockerTag(image))
		})
	}
}
