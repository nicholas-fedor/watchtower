package rebuild

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/git"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestSession_Check verifies the Git staleness check. It checks the result the
// session stores and the errors the check returns. It also checks the warning
// about an invalid Git label.
func TestSession_Check(t *testing.T) {
	t.Parallel()

	t.Run("treats an unassociated container as fresh and stores the empty result", func(t *testing.T) {
		t.Parallel()

		sess := NewSession(gitTestClient(t))
		c := mockUpdate.CreateMockContainer("plain", "/plain", "nginx:latest", time.Now())

		stale, newest, err := sess.Check(newTestLogger(t), t.Context(), c, types.UpdateParams{})
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

		var buf bytes.Buffer

		log := zerolog.New(&buf).Level(zerolog.WarnLevel)

		stale, newest, err := sess.Check(&log, ctx, c, types.UpdateParams{
			EnableGitMonitoring: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "git check:")
		assert.False(t, stale)
		assert.Empty(t, newest)
		assert.Empty(t, buf.String(), "an unreachable remote is not an invalid Git label")
	})

	t.Run("warns about an invalid git label without its raw value", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			labels      map[string]string
			wantErr     error
			wantWarning string
		}{
			{
				name:        "semver policy",
				labels:      map[string]string{labels.GitSemverPolicy: "nightly"},
				wantErr:     git.ErrInvalidPolicy,
				wantWarning: "Skipped container with an invalid git semver policy",
			},
			{
				name:        "git-host",
				labels:      map[string]string{labels.GitHost: "ftp://user:supersecret@evil.example"},
				wantErr:     git.ErrInvalidHost,
				wantWarning: "Skipped container with an invalid git-host",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				var buf bytes.Buffer

				log := zerolog.New(&buf).Level(zerolog.WarnLevel)
				sess := NewSession(gitTestClient(t))
				c := gitAssociatedContainer("app", "/app", "app:latest", tt.labels)

				_, _, err := sess.Check(&log, t.Context(), c, types.UpdateParams{EnableGitMonitoring: true})
				require.ErrorIs(t, err, tt.wantErr)

				assert.Contains(t, buf.String(), tt.wantWarning)
				assert.Contains(t, buf.String(), `"container":"app"`)
				assert.Contains(t, buf.String(), `"image":"app:latest"`)
				assert.NotContains(t, buf.String(), "supersecret")
			})
		}
	})
}
