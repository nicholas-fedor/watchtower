package rebuild

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/git"
	dockerConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/docker"
	gitConfig "github.com/nicholas-fedor/watchtower/internal/settings/config/git"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// TestSession_Check verifies the Git staleness check. It checks the result the
// session stores and the errors the check returns.
func TestSession_Check(t *testing.T) {
	t.Parallel()

	t.Run("treats an unassociated container as fresh and stores the empty result", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t), dockerConfig.Docker{})
		c := mockUpdate.CreateMockContainer("plain", "/plain", "nginx:latest", time.Now())

		stale, newest, err := sess.Check(t.Context(), c, updateParams.Params{})
		require.NoError(t, err)
		assert.False(t, stale)
		assert.Equal(t, c.ImageID(), newest)

		stored, ok := sess.result(c.ID())
		assert.True(t, ok)
		assert.False(t, stored.Stale)
	})

	t.Run("returns a wrapped error when the remote cannot be listed", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(git.New(newTestLogger(t), git.Options{Timeout: 50 * time.Millisecond}), dockerConfig.Docker{})
		c := gitAssociatedContainer("bad", "/bad", "myapp:latest", map[string]string{
			labels.GitRepo: "https://127.0.0.1:1/org/app.git",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()

		stale, newest, err := sess.Check(ctx, c, updateParams.Params{
			Git: gitConfig.Defaults{
				Enable: true,
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git check:")
		assert.False(t, stale)
		assert.Empty(t, newest)
	})

	t.Run("names an invalid git-host once", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t), dockerConfig.Docker{})
		c := gitAssociatedContainer("bad-host", "/bad-host", "myapp:latest", map[string]string{
			labels.GitRepo: "https://git.example.com/org/app.git",
			labels.GitHost: "ftp://git.example.com",
		})

		_, _, err := sess.Check(t.Context(), c, updateParams.Params{Git: gitConfig.Defaults{Enable: true}})
		require.ErrorIs(t, err, provenance.ErrInvalidHost)
		require.EqualError(t, err, "git check: invalid git-host: scheme must be http or https")
	})
}

// Test_gitImageID verifies the image ID that stands for a commit, including
// the fallback for a check that reported no commit.
func Test_gitImageID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		commit string
		want   container.ImageID
	}{
		{name: "commit", commit: "abc123", want: "git:abc123"},
		{name: "no commit", commit: "", want: "git:unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, gitImageID(tt.commit))
		})
	}
}
