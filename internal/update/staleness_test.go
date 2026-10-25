package update

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// Test_shouldUpdateContainer verifies that Watchtower updates only itself among
// Watchtower containers, matched by container ID, and leaves other containers
// alone. A scope label does not change the result, because the scope filter
// runs before this check.
func Test_shouldUpdateContainer(t *testing.T) {
	t.Parallel()

	t.Run("should allow self-update of current Watchtower container", func(t *testing.T) {
		t.Parallel()

		currentID := currentWatchtowerID
		container := mockUpdate.CreateMockContainerWithConfig(
			currentID,
			"watchtower-current",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(container, true, params)
		assert.True(t, result)
	})

	t.Run("should skip other Watchtower containers from self-updates", func(t *testing.T) {
		t.Parallel()

		currentID := currentWatchtowerID
		otherID := otherWatchtowerID
		container := mockUpdate.CreateMockContainerWithConfig(
			otherID,
			"watchtower-other",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(container, true, params)
		assert.False(t, result)
	})

	t.Run("should not affect non-Watchtower containers", func(t *testing.T) {
		t.Parallel()

		currentID := currentWatchtowerID
		container := mockUpdate.CreateMockContainerWithConfig(
			"non-watchtower-id",
			"nginx",
			"nginx:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(container, true, params)
		assert.True(t, result)
	})

	t.Run("should allow self-update of current Watchtower container with a scope label", func(t *testing.T) {
		t.Parallel()

		currentID := currentWatchtowerID
		container := mockUpdate.CreateMockContainerWithConfig(
			currentID,
			"watchtower-current",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(container, true, params)
		assert.True(t, result)
	})

	t.Run("should skip other Watchtower containers with a scope label from self-updates", func(t *testing.T) {
		t.Parallel()

		currentID := currentWatchtowerID
		otherID := otherWatchtowerID
		container := mockUpdate.CreateMockContainerWithConfig(
			otherID,
			"watchtower-other",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower":       "true",
					"com.centurylinklabs.watchtower.scope": "prod",
				},
			},
		)
		params := types.UpdateParams{
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(container, true, params)
		assert.False(t, result)
	})

	t.Run("should allow update of non-Watchtower containers when NoRestart is true", func(t *testing.T) {
		t.Parallel()

		container := mockUpdate.CreateMockContainerWithConfig(
			"non-watchtower-id",
			"nginx",
			"nginx:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{},
			},
		)
		params := types.UpdateParams{
			NoRestart: true,
		}
		result := shouldUpdateContainer(container, true, params)
		assert.True(t, result)
	})

	t.Run("should allow update of Watchtower containers when NoRestart is true", func(t *testing.T) {
		t.Parallel()

		currentID := currentWatchtowerID
		container := mockUpdate.CreateMockContainerWithConfig(
			currentID,
			"watchtower-current",
			"watchtower:latest",
			true,
			false,
			time.Now(),
			&dockerContainer.Config{
				Labels: map[string]string{
					"com.centurylinklabs.watchtower": "true",
				},
			},
		)
		params := types.UpdateParams{
			NoRestart:          true,
			CurrentContainerID: types.ContainerID(currentID),
		}
		result := shouldUpdateContainer(container, true, params)
		assert.True(t, result)
	})
}

// Test_isPinned verifies which image references count as pinned by digest.
func Test_isPinned(t *testing.T) {
	t.Parallel()

	// Valid 64-hex digest used by Docker/OCI references.
	const fullDigest = "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"

	t.Run("digest pin detection via shared helper", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			image      string
			wantPinned bool
		}{
			{name: "tagged image", image: "nginx:latest", wantPinned: false},
			{name: "repo@digest", image: "nginx@" + fullDigest, wantPinned: true},
			{name: "tag and digest", image: "nginx:1.27@" + fullDigest, wantPinned: true},
			{name: "registry/repo@digest", image: "registry.example.com/org/app@" + fullDigest, wantPinned: true},
			{name: "bare content digest", image: fullDigest, wantPinned: true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				image, wantPinned := tt.image, tt.wantPinned

				cont := mockUpdate.CreateMockContainer(
					"id1",
					"/name",
					image,
					time.Now(),
				)
				progress := state.Progress{}
				params := types.UpdateParams{}

				pinned, err := isPinned(newTestLogger(t), cont, &progress, params, nil)
				require.NoError(t, err)
				assert.Equal(t, wantPinned, pinned)

				if wantPinned {
					require.Len(t, progress, 1, "pinned containers are marked scanned")
				} else {
					assert.Empty(t, progress, "unpinned containers are not scanned here")
				}
			})
		}
	})

	t.Run("detects pin before parse fallback can replace the image name", func(t *testing.T) {
		t.Parallel()

		// ImageName is a valid digest pin. Even if parse were flaky, pin must win.
		cont := mockUpdate.CreateMockContainer(
			"pinned-id",
			"/pinned-name",
			"library/nginx@"+fullDigest,
			time.Now(),
		)
		progress := state.Progress{}

		pinned, err := isPinned(newTestLogger(t), cont, &progress, types.UpdateParams{}, nil)
		require.NoError(t, err)
		assert.True(t, pinned)
		require.Len(t, progress, 1)
	})

	t.Run("returns error for unresolvable invalid image names", func(t *testing.T) {
		t.Parallel()

		cont := mockUpdate.CreateMockContainer(
			"InvalidContainer",
			"/InvalidContainer",
			":latest",
			time.Now(),
		)
		progress := state.Progress{}

		pinned, err := isPinned(newTestLogger(t), cont, &progress, types.UpdateParams{}, nil)
		require.Error(t, err)
		assert.False(t, pinned)
		assert.Empty(t, progress)
	})

	t.Run("does not treat a sha256 container name fallback as a digest pin", func(t *testing.T) {
		t.Parallel()

		// The image name does not parse, so isPinned falls back to the
		// container name. The fallback sha256:latest is a tag, not a digest.
		cont := mockUpdate.CreateMockContainer(
			"sha256-name-id",
			"/sha256",
			"Nginx:latest",
			time.Now(),
		)
		progress := state.Progress{}

		pinned, err := isPinned(newTestLogger(t), cont, &progress, types.UpdateParams{}, nil)
		require.NoError(t, err)
		assert.False(t, pinned)
		assert.Empty(t, progress)
	})

	t.Run("does not panic when ContainerInfo Config is nil", func(t *testing.T) {
		t.Parallel()

		cont := mockTypes.NewMockContainer(t)
		cont.On("Name").Return("/nil-config")
		cont.On("ImageName").Return("nginx:latest")
		cont.On("ContainerInfo").Return(&dockerContainer.InspectResponse{
			Config: nil,
		})

		progress := state.Progress{}

		pinned, err := isPinned(newTestLogger(t), cont, &progress, types.UpdateParams{}, nil)
		require.NoError(t, err)
		assert.False(t, pinned)
	})
}
