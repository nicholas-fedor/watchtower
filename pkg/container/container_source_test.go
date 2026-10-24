package container

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/moby/moby/api/types/storage"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// testContainerID is the ID of the container the client tests list and inspect.
const testContainerID = "test-container-id"

// Test_listSourceContainers verifies the status filters the container list
// request carries for each client option, that a 404 from the list counts as
// no containers, and that a container whose image cannot be inspected is
// warned about only when it passes the filter.
func Test_listSourceContainers(t *testing.T) {
	t.Parallel()

	// expectListed expects one container list request filtered by exactly
	// the given statuses, and answers it with the test container.
	expectListed := func(api *mockContainer.MockAPIClient, statuses ...string) {
		api.EXPECT().ContainerList(mock.Anything, dockerClient.ContainerListOptions{Filters: statusFilters(statuses...)}).
			Return(dockerClient.ContainerListResult{Items: []dockerContainer.Summary{
				{ID: testContainerID, Names: []string{"/test-container"}, State: "running"},
			}}, nil).Once()
	}

	// expectInspects expects the inspects of the test container and its image.
	expectInspects := func(api *mockContainer.MockAPIClient) {
		api.EXPECT().ContainerInspect(mock.Anything, testContainerID, dockerClient.ContainerInspectOptions{}).
			Return(dockerClient.ContainerInspectResult{Container: dockerContainer.InspectResponse{
				ID:    testContainerID,
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
			Return(dockerClient.ImageInspectResult{ID: "test-image-id"}, nil).Once()
	}

	t.Run("no custom filter with default ClientOptions", func(t *testing.T) {
		t.Parallel()

		t.Run("should list only running containers", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectListed(api, "running")
			expectInspects(api)

			containers, err := listSourceContainers(newTestLogger(t), t.Context(), api, ClientOptions{}, nil)
			require.NoError(t, err)
			require.Len(t, containers, 1)
		})
	})

	t.Run("no custom filter with IncludeStopped=true", func(t *testing.T) {
		t.Parallel()

		t.Run("should list running, created, and exited containers", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectListed(api, "running", "created", "exited")
			expectInspects(api)

			containers, err := listSourceContainers(newTestLogger(t), t.Context(), api, ClientOptions{IncludeStopped: true}, nil)
			require.NoError(t, err)
			require.Len(t, containers, 1)
		})
	})

	t.Run("no custom filter with IncludeRestarting=true", func(t *testing.T) {
		t.Parallel()

		t.Run("should list running and restarting containers", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectListed(api, "running", "restarting")
			expectInspects(api)

			containers, err := listSourceContainers(newTestLogger(t), t.Context(), api, ClientOptions{IncludeRestarting: true}, nil)
			require.NoError(t, err)
			require.Len(t, containers, 1)
		})
	})

	t.Run("with custom filter (filters.NoFilter)", func(t *testing.T) {
		t.Parallel()

		t.Run("should list containers and apply custom filter", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectListed(api, "running")
			expectInspects(api)

			containers, err := listSourceContainers(newTestLogger(t), t.Context(), api, ClientOptions{}, filters.NoFilter)
			require.NoError(t, err)
			require.Len(t, containers, 1)
		})
	})

	t.Run("Docker API returns 404 NotFound for container list", func(t *testing.T) {
		t.Parallel()

		t.Run("should return empty container list without error", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			api.EXPECT().ContainerList(mock.Anything, mock.Anything).
				Return(dockerClient.ContainerListResult{}, notFoundError("page not found")).Once()

			containers, err := listSourceContainers(newTestLogger(t), t.Context(), api, ClientOptions{}, nil)
			require.NoError(t, err)
			assert.Empty(t, containers)
		})
	})

	t.Run("image inspection fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should not warn for containers excluded by the filter", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectListed(api, "running")
			expectMissingImage(api, testContainerID)

			log, logBuf := logcapture.New(zerolog.WarnLevel)
			excludeAll := filters.FilterByDisableNames(log, []string{"test-container"}, filters.NoFilter)

			containers, err := listSourceContainers(log, t.Context(), api, ClientOptions{}, excludeAll)
			require.NoError(t, err)
			assert.Empty(t, containers)
			assert.NotContains(t, logBuf.String(), "Failed to retrieve image info")
		})

		t.Run("should warn for containers that pass the filter", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectListed(api, "running")
			expectMissingImage(api, testContainerID)

			log, logBuf := logcapture.New(zerolog.WarnLevel)

			containers, err := listSourceContainers(log, t.Context(), api, ClientOptions{}, nil)
			require.NoError(t, err)
			require.Len(t, containers, 1)

			logged := logBuf.String()
			assert.Contains(t, logged, "Failed to retrieve image info")
			// The warning must identify the container by name and image, not just by ID.
			assert.Contains(t, logged, "test-container")
			assert.Contains(t, logged, "test-image:latest")
		})
	})
}

// Test_buildListFilterArgs verifies which container statuses the list filter
// requests for each combination of client options and runtime, including that
// Podman never gets the restarting status.
func Test_buildListFilterArgs(t *testing.T) {
	t.Parallel()

	t.Run("includes running status always", func(t *testing.T) {
		t.Parallel()

		opts := ClientOptions{}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := getStatusFilterKeys(filterArgs)
		assert.Contains(t, statuses, "running")
	})

	t.Run("includes created and exited when IncludeStopped is true", func(t *testing.T) {
		t.Parallel()

		opts := ClientOptions{IncludeStopped: true}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := getStatusFilterKeys(filterArgs)
		assert.Contains(t, statuses, "created")
		assert.Contains(t, statuses, "exited")
		assert.Contains(t, statuses, "running")
	})

	t.Run("does not include created and exited when IncludeStopped is false", func(t *testing.T) {
		t.Parallel()

		opts := ClientOptions{IncludeStopped: false}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := getStatusFilterKeys(filterArgs)
		assert.NotContains(t, statuses, "created")
		assert.NotContains(t, statuses, "exited")
		assert.Contains(t, statuses, "running")
	})

	t.Run("includes restarting when IncludeRestarting is true and isPodman is false", func(t *testing.T) {
		t.Parallel()

		opts := ClientOptions{IncludeRestarting: true}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := getStatusFilterKeys(filterArgs)
		assert.Contains(t, statuses, "restarting")
		assert.Contains(t, statuses, "running")
	})

	t.Run("does not include restarting when IncludeRestarting is false", func(t *testing.T) {
		t.Parallel()

		opts := ClientOptions{IncludeRestarting: false}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := getStatusFilterKeys(filterArgs)
		assert.NotContains(t, statuses, "restarting")
		assert.Contains(t, statuses, "running")
	})

	t.Run("does not include restarting when isPodman is true regardless of IncludeRestarting", func(t *testing.T) {
		t.Parallel()

		opts := ClientOptions{IncludeRestarting: true}
		filterArgs := buildListFilterArgs(opts, true)
		statuses := getStatusFilterKeys(filterArgs)
		assert.NotContains(t, statuses, "restarting")
		assert.Contains(t, statuses, "running")
	})

	t.Run("includes all statuses when IncludeStopped and IncludeRestarting are true and isPodman is false", func(t *testing.T) {
		t.Parallel()

		opts := ClientOptions{IncludeStopped: true, IncludeRestarting: true}
		filterArgs := buildListFilterArgs(opts, false)
		statuses := getStatusFilterKeys(filterArgs)
		assert.Contains(t, statuses, "running")
		assert.Contains(t, statuses, "created")
		assert.Contains(t, statuses, "exited")
		assert.Contains(t, statuses, "restarting")
	})

	t.Run("includes running, created, exited but not restarting when IncludeStopped is true, IncludeRestarting is true, and isPodman is true", func(t *testing.T) {
		t.Parallel()

		opts := ClientOptions{IncludeStopped: true, IncludeRestarting: true}
		filterArgs := buildListFilterArgs(opts, true)
		statuses := getStatusFilterKeys(filterArgs)
		assert.Contains(t, statuses, "running")
		assert.Contains(t, statuses, "created")
		assert.Contains(t, statuses, "exited")
		assert.NotContains(t, statuses, "restarting")
	})
}

// sourceInspect returns the inspect response of a source container on
// test-image:latest.
//
// Parameters:
//   - name: Docker name of the container.
//   - running: Whether the container is running.
//   - hostConfig: Host configuration of the container.
//
// Returns:
//   - dockerContainer.InspectResponse: Inspect response of testContainerID.
func sourceInspect(name string, running bool, hostConfig *dockerContainer.HostConfig) dockerContainer.InspectResponse {
	status := dockerContainer.StateRunning
	if !running {
		status = dockerContainer.StateExited
	}

	return dockerContainer.InspectResponse{
		ID:         testContainerID,
		Name:       name,
		Image:      "test-image:latest",
		State:      &dockerContainer.State{Status: status, Running: running},
		HostConfig: hostConfig,
		Config:     &dockerContainer.Config{Image: "test-image:latest"},
	}
}

// expectInspect expects one inspect of the container and answers it with info
// and err.
//
// Parameters:
//   - api: Mocked Docker API.
//   - containerID: ID of the container to inspect.
//   - info: Inspect response.
//   - err: Error the inspect returns.
func expectInspect(api *mockContainer.MockAPIClient, containerID string, info dockerContainer.InspectResponse, err error) {
	api.EXPECT().ContainerInspect(mock.Anything, containerID, dockerClient.ContainerInspectOptions{}).
		Return(dockerClient.ContainerInspectResult{Container: info}, err).Once()
}

// expectTestImage expects one inspect of test-image:latest and answers it with
// the image test-image-id.
//
// Parameters:
//   - api: Mocked Docker API.
func expectTestImage(api *mockContainer.MockAPIClient) {
	api.EXPECT().ImageInspect(mock.Anything, "test-image:latest").
		Return(dockerClient.ImageInspectResult{ID: "test-image-id"}, nil).Once()
}

// TestGetSourceContainer verifies that a source container is inspected with
// its image, keeps working without image info, has its network mode and
// volumes-from references rewritten from IDs to names where they resolve, and
// fails when the container inspect fails.
func TestGetSourceContainer(t *testing.T) {
	t.Parallel()

	const (
		appID = "9b738d6a78250d731816cc5020d8cfaf55e605cd510f4d4662688bd9a6c4e0c7"
		dbID  = "a1b2c3d4e5f60718293a4b5c6d7e8f901234567890abcdef1234567890abcd"
		// missingID is a volumes-from source that no longer exists.
		missingID = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	)

	// expectNamed expects one inspect of a referenced container and answers
	// it with the container's name.
	expectNamed := func(api *mockContainer.MockAPIClient, id, name string) {
		expectInspect(api, id, dockerContainer.InspectResponse{ID: id, Name: name}, nil)
	}

	// expectMissing expects one inspect of a referenced container that no
	// longer exists.
	expectMissing := func(api *mockContainer.MockAPIClient, id string) {
		expectInspect(api, id, dockerContainer.InspectResponse{}, notFoundError("No such container"))
	}

	// volumesFrom returns the volumes-from host config of the source.
	volumesFrom := func(entries ...string) *dockerContainer.HostConfig {
		return &dockerContainer.HostConfig{VolumesFrom: entries}
	}

	t.Run("retrieving a running container successfully", func(t *testing.T) {
		t.Parallel()

		t.Run("should return container with image info", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, sourceInspect("/test-watchtower", true, &dockerContainer.HostConfig{}), nil)
			expectTestImage(api)

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.NoError(t, err)
			require.NotNil(t, container)
			assert.Equal(t, types.ContainerID(testContainerID), container.ID())
			assert.Equal(t, "test-watchtower", container.Name())
			assert.Equal(t, types.ImageID("test-image-id"), container.ImageID())
		})
	})

	t.Run("retrieving a stopped container successfully", func(t *testing.T) {
		t.Parallel()

		t.Run("should return container without image info when ImageInspect fails", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, sourceInspect("/test-watchtower", false, &dockerContainer.HostConfig{}), nil)
			api.EXPECT().ImageInspect(mock.Anything, "test-image:latest").
				Return(dockerClient.ImageInspectResult{}, daemonError("image not found")).Once()

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.NoError(t, err)
			require.NotNil(t, container)
			assert.Equal(t, types.ContainerID(testContainerID), container.ID())
			assert.Equal(t, types.ImageID(""), container.ImageID())
		})
	})

	t.Run("container has network mode requiring resolution", func(t *testing.T) {
		t.Parallel()

		t.Run("should resolve network container name", func(t *testing.T) {
			t.Parallel()

			const parentID = "parent-container-id"

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, sourceInspect("/test-watchtower", true, &dockerContainer.HostConfig{
				NetworkMode: dockerContainer.NetworkMode("container:" + parentID),
			}), nil)
			expectNamed(api, parentID, "/parent-container")
			expectTestImage(api)

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.NoError(t, err)
			require.NotNil(t, container)
			assert.Equal(t, dockerContainer.NetworkMode("container:/parent-container"), container.ContainerInfo().HostConfig.NetworkMode)
		})
	})

	t.Run("container has volumes-from requiring resolution", func(t *testing.T) {
		t.Parallel()

		t.Run("should rewrite volumes-from container ID to name", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, sourceInspect("/nextcloud-nginx", true, volumesFrom(appID+":rw")), nil)
			expectNamed(api, appID, "/nextcloud-app")
			expectTestImage(api)

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{"nextcloud-app:rw"}, container.ContainerInfo().HostConfig.VolumesFrom)
			assert.Equal(t, []string{"nextcloud-app:rw"}, container.GetCreateHostConfig().VolumesFrom)
		})

		t.Run("should leave volumes-from ID when the source cannot be resolved", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, sourceInspect("/nextcloud-nginx", true, volumesFrom(appID)), nil)
			expectMissing(api, appID)
			expectTestImage(api)

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{appID}, container.ContainerInfo().HostConfig.VolumesFrom)
		})

		t.Run("should rewrite volumes-from IDs and preserve ro mode", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, sourceInspect("/nextcloud-nginx", true, volumesFrom(appID+":ro")), nil)
			expectNamed(api, appID, "/nextcloud-app")
			expectTestImage(api)

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{"nextcloud-app:ro"}, container.ContainerInfo().HostConfig.VolumesFrom)
		})

		t.Run("should rewrite multiple volumes-from entries independently", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, sourceInspect("/nextcloud-nginx", true, volumesFrom(appID+":rw", dbID)), nil)
			expectNamed(api, appID, "/nextcloud-app")
			expectNamed(api, dbID, "/nextcloud-db")
			expectTestImage(api)

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{"nextcloud-app:rw", "nextcloud-db"}, container.ContainerInfo().HostConfig.VolumesFrom)
		})

		t.Run("should rewrite resolved volumes-from entries and leave unresolved IDs", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, sourceInspect("/nextcloud-nginx", true, volumesFrom(appID+":rw", missingID)), nil)
			expectNamed(api, appID, "/nextcloud-app")
			expectMissing(api, missingID)
			expectTestImage(api)

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{"nextcloud-app:rw", missingID}, container.ContainerInfo().HostConfig.VolumesFrom)
		})
	})

	t.Run("container ID is invalid", func(t *testing.T) {
		t.Parallel()

		t.Run("should return error for 404 not found", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, "invalid-container-id", dockerContainer.InspectResponse{}, notFoundError("No such container"))

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, "invalid-container-id", nil)
			require.ErrorContains(t, err, "failed to inspect container")
			assert.Nil(t, container)
		})
	})

	t.Run("API connection fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should return error for connection timeout", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectInspect(api, testContainerID, dockerContainer.InspectResponse{}, daemonError("gateway timeout"))

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.ErrorContains(t, err, "failed to inspect container")
			assert.Nil(t, container)
		})
	})

	t.Run("response is malformed", func(t *testing.T) {
		t.Parallel()

		t.Run("should return error for invalid JSON", func(t *testing.T) {
			t.Parallel()

			// The SDK reports a response it cannot decode as an error.
			api := newDockerAPI(t)
			expectInspect(api, testContainerID, dockerContainer.InspectResponse{},
				errors.New("invalid character 'j' looking for beginning of value"))

			container, err := getSourceContainer(newTestLogger(t), t.Context(), api, testContainerID, nil)
			require.ErrorContains(t, err, "failed to inspect container")
			assert.Nil(t, container)
		})
	})
}

// Test_stopAndRemoveSourceContainer verifies that a running container is
// stopped with its signal and timeout and then force-removed, with volumes
// when asked, that a container Docker removes on exit is only stopped, and
// that stop and remove failures are reported.
func Test_stopAndRemoveSourceContainer(t *testing.T) {
	t.Parallel()

	// expectRemove expects one forced removal of the mock container and
	// answers it with err.
	expectRemove := func(api *mockContainer.MockAPIClient, removeVolumes bool, err error) {
		api.EXPECT().
			ContainerRemove(mock.Anything, mockContainerID, dockerClient.ContainerRemoveOptions{Force: true, RemoveVolumes: removeVolumes}).
			Return(dockerClient.ContainerRemoveResult{}, err).Once()
	}

	// running returns a running container with the given updates.
	running := func(updates ...containerfixture.Update) *container.Container {
		return modelfixture.Container(append([]containerfixture.Update{containerfixture.WithContainerState(dockerContainer.State{Running: true})}, updates...)...)
	}

	t.Run("stopping and removing a running container successfully with removeVolumes=false", func(t *testing.T) {
		t.Parallel()

		t.Run("should stop and remove the container without error", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, nil)
			expectRemove(api, false, nil)

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second, false)
			require.NoError(t, err)
		})
	})

	t.Run("stopping and removing a running container successfully with removeVolumes=true", func(t *testing.T) {
		t.Parallel()

		t.Run("should stop and remove the container with volume removal", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, nil)
			expectRemove(api, true, nil)

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second, true)
			require.NoError(t, err)
		})
	})

	t.Run("stopping fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should return an error without attempting removal", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, daemonError("stop failed"))

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second, false)
			require.ErrorContains(t, err, "failed to stop container")
		})
	})

	t.Run("removing fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should return an error after successful stop", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, nil)
			expectRemove(api, false, daemonError("remove failed"))

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second, false)
			require.ErrorContains(t, err, "failed to remove container")
		})
	})

	t.Run("container has AutoRemove enabled", func(t *testing.T) {
		t.Parallel()

		t.Run("should stop but skip removal when running", func(t *testing.T) {
			t.Parallel()

			// Docker removes an AutoRemove container after it stops, so only
			// the stop is expected.
			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, nil)

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, running(containerfixture.WithAutoRemove(true)), 10*time.Second, true)
			require.NoError(t, err)
		})

		t.Run("should remove explicitly when not running", func(t *testing.T) {
			t.Parallel()

			// AutoRemove does not apply to a container that never started.
			api := newDockerAPI(t)
			expectRemove(api, false, nil)

			container := modelfixture.Container(
				containerfixture.WithContainerState(dockerContainer.State{Running: false, Status: "created"}),
				containerfixture.WithAutoRemove(true),
			)

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, container, 10*time.Second, false)
			require.NoError(t, err)
		})
	})

	t.Run("container HostConfig is nil", func(t *testing.T) {
		t.Parallel()

		t.Run("should stop and remove without panicking", func(t *testing.T) {
			t.Parallel()

			// A nil HostConfig is not AutoRemove, so the container is removed.
			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, nil)
			expectRemove(api, true, nil)

			container := running(func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
				c.HostConfig = nil
			})

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, container, 10*time.Second, true)
			require.NoError(t, err)
		})
	})

	t.Run("container is not running", func(t *testing.T) {
		t.Parallel()

		t.Run("should skip stop and remove successfully", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectRemove(api, true, nil)

			container := modelfixture.Container(containerfixture.WithContainerState(dockerContainer.State{Running: false}))

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, container, 10*time.Second, true)
			require.NoError(t, err)
		})
	})

	t.Run("timeout is specified", func(t *testing.T) {
		t.Parallel()

		t.Run("should pass timeout to stop operation", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 30, nil)
			expectRemove(api, false, nil)

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, running(), 30*time.Second, false)
			require.NoError(t, err)
		})
	})

	t.Run("container is not found during removal", func(t *testing.T) {
		t.Parallel()

		t.Run("should succeed as container is already gone", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, nil)
			expectRemove(api, false, notFoundError("No such container"))

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second, false)
			require.NoError(t, err)
		})
	})

	t.Run("API connection fails during stop", func(t *testing.T) {
		t.Parallel()

		t.Run("should return error", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, daemonError("gateway timeout"))

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second, false)
			require.ErrorContains(t, err, "failed to stop container")
		})
	})
}

// Test_stopSourceContainer verifies that a running container is stopped with
// its signal and timeout, that a stopped one is left alone, and that stop
// failures, including a container already gone, are reported.
func Test_stopSourceContainer(t *testing.T) {
	t.Parallel()

	// running returns a running container.
	running := func() *container.Container {
		return modelfixture.Container(containerfixture.WithContainerState(dockerContainer.State{Running: true}))
	}

	t.Run("stopping a linked restarting container", func(t *testing.T) {
		t.Parallel()

		t.Run("should log 'Stopping linked container' and succeed", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, nil)

			container := modelfixture.Container()
			container.SetLinkedToRestarting(true)

			log, logbuf := logcapture.New(zerolog.InfoLevel)

			err := stopSourceContainer(log, t.Context(), api, container, 10*time.Second)
			require.NoError(t, err)
			assert.Contains(t, logbuf.String(), "Stopping linked container")
		})
	})

	t.Run("stopping a running container successfully", func(t *testing.T) {
		t.Parallel()

		t.Run("should stop the container without error", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, nil)

			err := stopSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second)
			require.NoError(t, err)
		})
	})

	t.Run("stopping a container that fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should return an error", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, daemonError("stop failed"))

			err := stopSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second)
			require.ErrorContains(t, err, "failed to stop container")
		})
	})

	t.Run("stopping a container with timeout parameter", func(t *testing.T) {
		t.Parallel()

		t.Run("should pass the timeout to the Docker API", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 30, nil)

			err := stopSourceContainer(newTestLogger(t), t.Context(), api, running(), 30*time.Second)
			require.NoError(t, err)
		})
	})

	t.Run("stopping a container that is not running", func(t *testing.T) {
		t.Parallel()

		t.Run("should not attempt to stop and return no error", func(t *testing.T) {
			t.Parallel()

			// The mock has no expectations, so any Docker call fails the test.
			api := mockContainer.NewMockAPIClient(t)
			container := modelfixture.Container(containerfixture.WithContainerState(dockerContainer.State{Running: false}))

			err := stopSourceContainer(newTestLogger(t), t.Context(), api, container, 10*time.Second)
			require.NoError(t, err)
		})
	})

	t.Run("stopping a container that was removed between check and stop", func(t *testing.T) {
		t.Parallel()

		t.Run("should return a not-found error when Docker returns 404 NotFound", func(t *testing.T) {
			t.Parallel()

			api := newDockerAPI(t)
			expectStop(api, mockContainerID, 10, notFoundError("No such container"))

			err := stopSourceContainer(newTestLogger(t), t.Context(), api, running(), 10*time.Second)
			require.Error(t, err)
			assert.True(t, cerrdefs.IsNotFound(err))
		})
	})
}

// getStatusFilterKeys returns the statuses a container list filter requests.
//
// Parameters:
//   - f: Filter to read.
//
// Returns:
//   - []string: Requested statuses, in no particular order.
func getStatusFilterKeys(f dockerClient.Filters) []string {
	inner, ok := f["status"]
	if !ok {
		return nil
	}

	keys := make([]string, 0, len(inner))
	for k := range inner {
		keys = append(keys, k)
	}

	return keys
}

func TestCloneImageInspect_IsolatesMutations(t *testing.T) {
	t.Parallel()

	src := &dockerImage.InspectResponse{
		ID:          "sha256:abc",
		RepoDigests: []string{"app@sha256:abc"},
		RepoTags:    []string{"app:latest"},
		Config: &dockerspec.DockerOCIImageConfig{
			Env:        []string{"PATH=/usr/bin"},
			Cmd:        []string{"app"},
			Entrypoint: []string{"/bin/sh"},
			Labels:     map[string]string{"app": "web"},
			Volumes:    map[string]struct{}{"/data": {}},
			ExposedPorts: map[string]struct{}{
				"80/tcp": {},
			},
			Healthcheck: &dockerspec.HealthcheckConfig{Test: []string{"CMD", "true"}},
		},
		GraphDriver: &storage.DriverData{
			Name: "overlay2",
			Data: map[string]string{"MergedDir": "/var/lib/docker/overlay2/abc"},
		},
	}

	cloned := cloneImageInspect(src)
	require.NotNil(t, cloned)
	require.NotSame(t, src, cloned)

	cloned.RepoDigests[0] = "app@sha256:mutated"
	cloned.RepoTags = append(cloned.RepoTags, "app:dev")
	cloned.Config.Env[0] = "PATH=/mutated"
	cloned.Config.Cmd[0] = "mutated"
	cloned.Config.Entrypoint[0] = "/bin/mutated"
	cloned.Config.Labels["app"] = "mutated"
	cloned.Config.Volumes["/tmp"] = struct{}{}
	delete(cloned.Config.ExposedPorts, "80/tcp")
	cloned.Config.Healthcheck.Test[0] = "NONE"
	cloned.GraphDriver.Data["MergedDir"] = "/mutated"

	require.Equal(t, []string{"app@sha256:abc"}, src.RepoDigests)
	require.Equal(t, []string{"app:latest"}, src.RepoTags)
	require.Equal(t, []string{"PATH=/usr/bin"}, src.Config.Env)
	require.Equal(t, []string{"app"}, src.Config.Cmd)
	require.Equal(t, []string{"/bin/sh"}, src.Config.Entrypoint)
	require.Equal(t, map[string]string{"app": "web"}, src.Config.Labels)
	require.Equal(t, map[string]struct{}{"/data": {}}, src.Config.Volumes)
	require.Equal(t, map[string]struct{}{"80/tcp": {}}, src.Config.ExposedPorts)
	require.Equal(t, []string{"CMD", "true"}, src.Config.Healthcheck.Test)
	require.Equal(t, "/var/lib/docker/overlay2/abc", src.GraphDriver.Data["MergedDir"])
	require.Nil(t, cloneImageInspect(nil))
}

// newImageInspectFailAPI returns a mocked Docker API that lists one running
// container and fails the inspect of its image with err.
//
// Parameters:
//   - t: Test the mock belongs to.
//   - err: Error the image inspect fails with.
//
// Returns:
//   - *mockContainer.MockAPIClient: Mocked Docker API.
func newImageInspectFailAPI(t *testing.T, err error) *mockContainer.MockAPIClient {
	t.Helper()

	api := mockContainer.NewMockAPIClient(t)
	api.EXPECT().ClientVersion().Return("1.44").Maybe()
	api.EXPECT().ContainerList(mock.Anything, mock.Anything).
		Return(dockerClient.ContainerListResult{Items: []dockerContainer.Summary{{ID: testContainerID}}}, nil).Once()
	api.EXPECT().ContainerInspect(mock.Anything, testContainerID, dockerClient.ContainerInspectOptions{}).
		Return(dockerClient.ContainerInspectResult{Container: dockerContainer.InspectResponse{
			ID:         testContainerID,
			Name:       "/app",
			Image:      "sha256:image",
			State:      &dockerContainer.State{Running: true},
			Config:     &dockerContainer.Config{Image: "app:latest"},
			HostConfig: &dockerContainer.HostConfig{},
		}}, nil).Once()
	api.EXPECT().ImageInspect(mock.Anything, "sha256:image").
		Return(dockerClient.ImageInspectResult{}, err).Once()

	return api
}

// TestListSourceContainers_ImageInspectFailure verifies that a canceled or
// timed-out image inspect ends the listing, while other failures leave the
// container without image info. Ending the listing keeps an interrupted scan
// from warning about every remaining container.
func TestListSourceContainers_ImageInspectFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// inspectErr is the error the image inspect fails with.
		inspectErr error
		// wantErr is the error the listing returns. Nil expects the container
		// without image info.
		wantErr error
	}{
		{
			name:       "canceled",
			inspectErr: fmt.Errorf("request canceled: %w", context.Canceled),
			wantErr:    context.Canceled,
		},
		{
			name:       "timed out",
			inspectErr: fmt.Errorf("request timed out: %w", context.DeadlineExceeded),
			wantErr:    context.DeadlineExceeded,
		},
		{
			name:       "image not found",
			inspectErr: fmt.Errorf("no such image: %w", cerrdefs.ErrNotFound),
		},
		{
			name:       "daemon error",
			inspectErr: errors.New("daemon error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			log := zerolog.New(&out).Level(zerolog.WarnLevel)
			api := newImageInspectFailAPI(t, tt.inspectErr)

			containers, err := listSourceContainers(&log, t.Context(), api, ClientOptions{}, nil)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, containers)
				assert.NotContains(t, out.String(), "Failed to retrieve image info")

				return
			}

			require.NoError(t, err)
			require.Len(t, containers, 1)
			assert.False(t, containers[0].HasImageInfo())
			assert.Contains(t, out.String(), "Failed to retrieve image info")
		})
	}
}

// expectStop expects one stop of the container with the default stop signal
// and the given timeout, and answers it with err.
//
// Parameters:
//   - api: Mocked Docker API.
//   - containerID: ID of the container to stop.
//   - timeout: Stop timeout in seconds.
//   - err: Error the stop returns, nil for a successful stop.
func expectStop(api *mockContainer.MockAPIClient, containerID string, timeout int, err error) {
	api.EXPECT().ContainerStop(mock.Anything, containerID, dockerClient.ContainerStopOptions{
		Signal:  defaultStopSignal,
		Timeout: &timeout,
	}).Return(dockerClient.ContainerStopResult{}, err).Once()
}

// stopRemoveContainer returns a container in the given state for the stop and
// remove tests.
func stopRemoveContainer(running, autoRemove bool) *container.Container {
	return container.NewContainer(nil, &dockerContainer.InspectResponse{
		ID:         testContainerID,
		Name:       "/app",
		State:      &dockerContainer.State{Running: running},
		Config:     &dockerContainer.Config{Image: "app:latest"},
		HostConfig: &dockerContainer.HostConfig{AutoRemove: autoRemove},
	}, nil)
}

// TestStopAndRemoveSourceContainer_NotFound verifies that a container which no
// longer exists before Watchtower stops or removes it is reported as not
// found, while a container that disappears only after Watchtower stopped it,
// such as one Docker removes on exit, counts as stopped and removed.
func TestStopAndRemoveSourceContainer_NotFound(t *testing.T) {
	t.Parallel()

	notFound := fmt.Errorf("no such container: %w", cerrdefs.ErrNotFound)

	tests := []struct {
		name       string
		running    bool
		autoRemove bool
		stopErr    error
		removeErr  error
		// wantNotFound expects a not-found error. Otherwise no error is expected.
		wantNotFound bool
		// wantRemoved expects the container to be removed.
		wantRemoved bool
	}{
		{name: "running container gone before stop", running: true, stopErr: notFound, wantNotFound: true},
		{name: "running container removed after stop", running: true, removeErr: notFound, wantRemoved: true},
		{name: "stopped container gone before remove", removeErr: notFound, wantNotFound: true, wantRemoved: true},
		{name: "stopped container removed", wantRemoved: true},
		{name: "running auto-remove container", running: true, autoRemove: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api := mockContainer.NewMockAPIClient(t)
			if tt.running {
				expectStop(api, testContainerID, 1, tt.stopErr)
			}

			if tt.wantRemoved {
				api.EXPECT().
					ContainerRemove(mock.Anything, testContainerID, dockerClient.ContainerRemoveOptions{Force: true}).
					Return(dockerClient.ContainerRemoveResult{}, tt.removeErr).Once()
			}

			err := stopAndRemoveSourceContainer(newTestLogger(t), t.Context(), api,
				stopRemoveContainer(tt.running, tt.autoRemove), time.Second, false)

			if tt.wantNotFound {
				require.Error(t, err)
				assert.True(t, cerrdefs.IsNotFound(err), "error: %v", err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestStopSourceContainer_NotFound verifies that stopping a container which no
// longer exists reports it as not found.
func TestStopSourceContainer_NotFound(t *testing.T) {
	t.Parallel()

	api := mockContainer.NewMockAPIClient(t)
	expectStop(api, testContainerID, 1, fmt.Errorf("no such container: %w", cerrdefs.ErrNotFound))

	err := stopSourceContainer(newTestLogger(t), t.Context(), api, stopRemoveContainer(true, false), time.Second)
	require.Error(t, err)
	assert.True(t, cerrdefs.IsNotFound(err), "error: %v", err)
}
