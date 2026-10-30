package rebuild

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/git"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestSession_Check verifies the Git staleness check. It checks the result the
// session stores and the errors the check returns.
func TestSession_Check(t *testing.T) {
	t.Parallel()

	t.Run("treats an unassociated container as fresh and stores the empty result", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := mockUpdate.CreateMockContainer("plain", "/plain", "nginx:latest", time.Now())

		stale, newest, err := sess.Check(t.Context(), c, types.UpdateParams{})
		require.NoError(t, err)
		assert.False(t, stale)
		assert.Equal(t, c.ImageID(), newest)

		stored, ok := sess.result(c.ID())
		assert.True(t, ok)
		assert.False(t, stored.Stale)
	})

	t.Run("returns a wrapped error when the remote cannot be listed", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(git.New(newTestLogger(t), git.Options{Timeout: 50 * time.Millisecond}))
		c := gitAssociatedContainer("bad", "/bad", "myapp:latest", map[string]string{
			labels.GitRepo: "https://127.0.0.1:1/org/app.git",
		})

		ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		defer cancel()

		stale, newest, err := sess.Check(ctx, c, types.UpdateParams{
			EnableGitMonitoring: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git check:")
		assert.False(t, stale)
		assert.Empty(t, newest)
	})
}
