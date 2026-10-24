package container

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerClient "github.com/moby/moby/client"

	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// expectInspectState expects the cleanup inspect of the target container and
// returns it in the given state.
//
// Parameters:
//   - api: Mocked Docker API.
//   - running: Whether the target container is running.
func expectInspectState(api *mockContainer.MockAPIClient, running bool) {
	status := dockerContainer.StateCreated
	if running {
		status = dockerContainer.StateRunning
	}

	api.EXPECT().ContainerInspect(mock.Anything, testTargetID, dockerClient.ContainerInspectOptions{}).
		Return(dockerClient.ContainerInspectResult{Container: dockerContainer.InspectResponse{
			ID:    testTargetID,
			State: &dockerContainer.State{Status: status, Running: running},
		}}, nil).Once()
}

// TestStartTargetContainer verifies how a replacement container is started, and
// how a failed start removes the container when it is still stopped.
func TestStartTargetContainer(t *testing.T) {
	t.Parallel()

	t.Run("should skip starting stopped container when reviveStopped is false", func(t *testing.T) {
		t.Parallel()

		log, logOutput := newCapturedLogger(zerolog.DebugLevel)
		source := MockContainer(
			containerfixture.WithNetworkMode("bridge"),
			containerfixture.WithContainerState(dockerContainer.State{Running: false, Status: "exited"}),
		)

		err := startTargetContainer(t.Context(), mockContainer.NewMockAPIClient(t), log, source, testTargetID, false)
		require.NoError(t, err)
		assert.Contains(t, logOutput.String(), "Created container, not starting due to stopped state")
	})
	t.Run("should handle ContainerStart failure", func(t *testing.T) {
		t.Parallel()

		log, logOutput := newCapturedLogger(zerolog.DebugLevel)
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerStart(mock.Anything, testTargetID, dockerClient.ContainerStartOptions{}).
			Return(dockerClient.ContainerStartResult{}, errors.New("start failed")).Once()
		expectInspectState(api, false)
		expectForceRemove(api)

		err := startTargetContainer(t.Context(), api, log, newRunningSource(), testTargetID, true)
		require.ErrorContains(t, err, "start failed")
		assert.Contains(t, logOutput.String(), "Failed to start new container")
	})
	t.Run("should skip cleanup when container is running after start timeout", func(t *testing.T) {
		t.Parallel()

		log, logOutput := newCapturedLogger(zerolog.DebugLevel)
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerStart(mock.Anything, testTargetID, dockerClient.ContainerStartOptions{}).
			Return(dockerClient.ContainerStartResult{}, context.DeadlineExceeded).Once()
		expectInspectState(api, true)

		err := startTargetContainer(t.Context(), api, log, newRunningSource(), testTargetID, true)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, logOutput.String(), "Skipped cleanup of running container after start error")
	})
	t.Run("should skip cleanup when inspect fails after transport error", func(t *testing.T) {
		t.Parallel()

		log, logOutput := newCapturedLogger(zerolog.DebugLevel)
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerStart(mock.Anything, testTargetID, dockerClient.ContainerStartOptions{}).
			Return(dockerClient.ContainerStartResult{}, errors.New("connection reset by peer")).Once()
		api.EXPECT().ContainerInspect(mock.Anything, testTargetID, dockerClient.ContainerInspectOptions{}).
			Return(dockerClient.ContainerInspectResult{}, errors.New("inspect transport failure")).Once()

		err := startTargetContainer(t.Context(), api, log, newRunningSource(), testTargetID, true)
		require.ErrorContains(t, err, "connection reset by peer")
		assert.Contains(t, logOutput.String(), "Failed to inspect container after start error")
	})
	t.Run("should handle ContainerStart Conflict error", func(t *testing.T) {
		t.Parallel()

		log, logOutput := newCapturedLogger(zerolog.DebugLevel)
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerStart(mock.Anything, testTargetID, dockerClient.ContainerStartOptions{}).
			Return(dockerClient.ContainerStartResult{}, cerrdefs.ErrConflict).Once()
		expectInspectState(api, false)
		expectForceRemove(api)

		err := startTargetContainer(t.Context(), api, log, newRunningSource(), testTargetID, true)
		require.ErrorContains(t, err, "failed to start container")
		assert.True(t, cerrdefs.IsConflict(err), "wrapped error should be detectable via cerrdefs.IsConflict")
		require.ErrorIs(t, err, cerrdefs.ErrConflict, "wrapped error should be detectable via errors.Is")
		assert.Contains(t, logOutput.String(), "Failed to start new container")
	})
	t.Run("should handle ContainerStart NotFound error", func(t *testing.T) {
		t.Parallel()

		log, _ := newCapturedLogger(zerolog.DebugLevel)
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerStart(mock.Anything, testTargetID, dockerClient.ContainerStartOptions{}).
			Return(dockerClient.ContainerStartResult{}, cerrdefs.ErrNotFound).Once()
		expectInspectState(api, false)
		expectForceRemove(api)

		err := startTargetContainer(t.Context(), api, log, newRunningSource(), testTargetID, true)
		require.ErrorContains(t, err, "failed to start container")
	})
	t.Run("should log successful container start", func(t *testing.T) {
		t.Parallel()

		log, logOutput := newCapturedLogger(zerolog.DebugLevel)
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().ContainerStart(mock.Anything, testTargetID, dockerClient.ContainerStartOptions{}).
			Return(dockerClient.ContainerStartResult{}, nil).Once()

		err := startTargetContainer(t.Context(), api, log, newRunningSource(), testTargetID, true)
		require.NoError(t, err)
		assert.Contains(t, logOutput.String(), "Started new container")
		assert.Contains(t, logOutput.String(), "new_id="+types.ContainerID(testTargetID).ShortID())
	})
}
