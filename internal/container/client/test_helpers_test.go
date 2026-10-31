package client

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/mock"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerClient "github.com/moby/moby/client"

	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
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

// expectMissingImage expects the inspect of a running container whose image can
// no longer be inspected, which happens when the image is removed out from
// under it.
//
// The container inspect succeeds and the image inspect reports not found, so
// callers end up with a container whose HasImageInfo reports false.
//
// Parameters:
//   - api: Mocked Docker API.
//   - containerID: ID of the container to inspect.
func expectMissingImage(api *mockContainer.MockAPIClient, containerID string) {
	api.EXPECT().ContainerInspect(mock.Anything, containerID, dockerClient.ContainerInspectOptions{}).
		Return(dockerClient.ContainerInspectResult{Container: dockerContainer.InspectResponse{
			ID:    containerID,
			Name:  "/test-container",
			Image: "test-image:latest",
			State: &dockerContainer.State{
				Status:  "running",
				Running: true,
			},
			HostConfig: &dockerContainer.HostConfig{},
			Config: &dockerContainer.Config{
				Image: "test-image:latest",
			},
		}}, nil).Once()
	api.EXPECT().ImageInspect(mock.Anything, "test-image:latest").
		Return(dockerClient.ImageInspectResult{}, notFoundError("No such image")).Once()
}
