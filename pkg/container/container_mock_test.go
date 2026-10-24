package container

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerNetwork "github.com/moby/moby/api/types/network"
	dockerSystem "github.com/moby/moby/api/types/system"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container"
	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// testTargetID is the ID the mocked daemon gives a created target container.
const testTargetID = "new_container_id"

// dockerImageFixture is an image whose inspect response is read from testdata.
type dockerImageFixture struct {
	// id is the image ID.
	id types.ImageID
	// file names testdata/image_<file>.json.
	file string
}

// dockerContainerFixture is a container whose inspect response is read from
// testdata, with its image and the containers it references.
type dockerContainerFixture struct {
	// image is the container's image. Nil for a missing container.
	image *dockerImageFixture
	// id is the container ID.
	id types.ContainerID
	// name names testdata/container_<name>.json unless file is set.
	name string
	// file overrides name as the testdata file name.
	file string
	// references are containers inspected while resolving this one, such as
	// a network mode container.
	references []*dockerContainerFixture
	// missing makes the daemon report the container as not found.
	missing bool
}

const (
	// fixtureNetSupplierNotFoundID is the ID of a network supplier container
	// that no longer exists.
	fixtureNetSupplierNotFoundID = "badc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc1dbadc"
	// fixtureNetSupplierContainerName is the Docker name of the network
	// supplier container.
	fixtureNetSupplierContainerName = "/wt-contnet-producer-1"
)

// Docker fixtures served from testdata.
var (
	// fixtureDefaultImage is the Watchtower image.
	fixtureDefaultImage = &dockerImageFixture{
		id:   "sha256:4dbc5f9c07028a985e14d1393e849ea07f68804c4293050d5a641b138db72daa",
		file: "default",
	}
	// fixtureWatchtower is a running Watchtower container.
	fixtureWatchtower = &dockerContainerFixture{
		name:  "watchtower",
		id:    "3d88e0e3543281c747d88b27e246578b65ae8964ba86c7cd7522cf84e0978134",
		image: fixtureDefaultImage,
	}
	// fixtureStopped is a stopped container.
	fixtureStopped = &dockerContainerFixture{
		name:  "stopped",
		id:    "ae8964ba86c7cd7522cf84e09781343d88e0e3543281c747d88b27e246578b65",
		image: fixtureDefaultImage,
	}
	// fixtureRunning is a running Portainer container.
	fixtureRunning = &dockerContainerFixture{
		name: "running",
		id:   "b978af0b858aa8855cce46b628817d4ed58e58f2c4f66c9b9c5449134ed4c008",
		image: &dockerImageFixture{
			id:   "sha256:19d07168491a3f9e2798a9bed96544e34d57ddc4757a4ac5bb199dea896c87fd",
			file: "running",
		},
	}
	// fixtureRestarting is a restarting container.
	fixtureRestarting = &dockerContainerFixture{
		name:  "restarting",
		id:    "ae8964ba86c7cd7522cf84e09781343d88e0e3543281c747d88b27e246578b67",
		image: fixtureDefaultImage,
	}
	// fixtureNetSupplier is a container supplying its network to another.
	fixtureNetSupplier = &dockerContainerFixture{
		name: "net_supplier",
		id:   "25e75393800b5c450a6841212a3b92ed28fa35414a586dec9f2c8a520d4910c2",
		image: &dockerImageFixture{
			id:   "sha256:c22b543d33bfdcb9992cbef23961677133cdf09da71d782468ae2517138bad51",
			file: "net_producer",
		},
	}
	// fixtureNetSupplierNotFound is a network supplier that no longer exists.
	fixtureNetSupplierNotFound = &dockerContainerFixture{
		name:    "net_supplier",
		id:      fixtureNetSupplierNotFoundID,
		missing: true,
	}
	// fixtureNetConsumerImage is the image of the network consumers.
	fixtureNetConsumerImage = &dockerImageFixture{
		id:   "sha256:904b8cb13b932e23230836850610fa45dce9eb0650d5618c2b1487c2a4f577b8",
		file: "net_consumer",
	}
	// fixtureNetConsumer uses the network of fixtureNetSupplier.
	fixtureNetConsumer = &dockerContainerFixture{
		name:       "net_consumer",
		id:         "1f6b79d2aff23244382026c76f4995851322bed5f9c50631620162f6f9aafbd6",
		image:      fixtureNetConsumerImage,
		references: []*dockerContainerFixture{fixtureNetSupplier},
	}
	// fixtureNetConsumerMissingSupplier uses the network of a container that
	// no longer exists.
	fixtureNetConsumerMissingSupplier = &dockerContainerFixture{
		name:       "net_consumer-missing_supplier",
		id:         fixtureNetConsumer.id,
		image:      fixtureNetConsumerImage,
		references: []*dockerContainerFixture{fixtureNetSupplierNotFound},
	}
)

// newDockerAPI returns a mocked Docker API whose client version is available
// for diagnostic logs.
//
// Parameters:
//   - t: Test the mock belongs to.
//
// Returns:
//   - *mockContainer.MockAPIClient: Mocked Docker API.
func newDockerAPI(t *testing.T) *mockContainer.MockAPIClient {
	t.Helper()

	api := mockContainer.NewMockAPIClient(t)
	api.EXPECT().ClientVersion().Return("1.47").Maybe()

	return api
}

// readFixture decodes testdata/name into a value of type T.
//
// Parameters:
//   - t: Test reading the fixture.
//   - name: File name within testdata.
//
// Returns:
//   - T: Decoded fixture.
func readFixture[T any](t *testing.T, name string) T {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	var value T

	require.NoError(t, json.Unmarshal(data, &value))

	return value
}

// daemonError returns an error formatted as the Docker SDK reports a daemon
// error response.
//
// Parameters:
//   - message: Message the daemon responded with.
//
// Returns:
//   - error: Error with the daemon's message.
func daemonError(message string) error {
	return errors.New("Error response from daemon: " + message)
}

// notFoundError returns a not-found error as the Docker SDK reports a 404.
//
// Parameters:
//   - message: Message the daemon responded with.
//
// Returns:
//   - error: Error matching [cerrdefs.IsNotFound].
func notFoundError(message string) error {
	return fmt.Errorf("Error response from daemon: %s: %w", message, cerrdefs.ErrNotFound)
}

// statusFilters returns list filters for the given container statuses.
//
// Parameters:
//   - statuses: Container statuses to list.
//
// Returns:
//   - dockerClient.Filters: Filters with one status entry per status.
func statusFilters(statuses ...string) dockerClient.Filters {
	filters := make(dockerClient.Filters)
	for _, status := range statuses {
		filters.Add("status", status)
	}

	return filters
}

// expectContainerList expects one container list request filtered by exactly
// the given statuses, and answers with the testdata containers in them.
//
// Parameters:
//   - t: Test the expectation belongs to.
//   - api: Mocked Docker API.
//   - statuses: Statuses the request filters by.
func expectContainerList(t *testing.T, api *mockContainer.MockAPIClient, statuses ...string) {
	t.Helper()

	var listed []dockerContainer.Summary

	for _, summary := range readFixture[[]dockerContainer.Summary](t, "containers.json") {
		for _, status := range statuses {
			if summary.State == dockerContainer.ContainerState(status) {
				listed = append(listed, summary)
			}
		}
	}

	api.EXPECT().ContainerList(mock.Anything, dockerClient.ContainerListOptions{Filters: statusFilters(statuses...)}).
		Return(dockerClient.ContainerListResult{Items: listed}, nil).Once()
}

// expectContainerFixtures expects one inspect of each container, of each
// container it references, and of each distinct image, and answers them from
// testdata.
//
// Parameters:
//   - t: Test the expectations belong to.
//   - api: Mocked Docker API.
//   - fixtures: Containers the code under test inspects.
func expectContainerFixtures(t *testing.T, api *mockContainer.MockAPIClient, fixtures ...*dockerContainerFixture) {
	t.Helper()

	seenImages := make(map[types.ImageID]bool, len(fixtures))

	for _, fixture := range fixtures {
		expectContainerFixture(t, api, fixture)

		for _, reference := range fixture.references {
			expectContainerFixture(t, api, reference)
		}

		if seenImages[fixture.image.id] {
			continue
		}

		seenImages[fixture.image.id] = true
		image := readFixture[dockerImage.InspectResponse](t, "image_"+fixture.image.file+".json")
		require.Equal(t, string(fixture.image.id), image.ID, "image fixture file %q", fixture.image.file)
		api.EXPECT().ImageInspect(mock.Anything, string(fixture.image.id)).
			Return(dockerClient.ImageInspectResult{InspectResponse: image}, nil).Once()
	}
}

// expectContainerFixture expects one inspect of the container and answers it
// from testdata, or with a not-found error for a missing container.
//
// Parameters:
//   - t: Test the expectation belongs to.
//   - api: Mocked Docker API.
//   - fixture: Container to inspect.
func expectContainerFixture(t *testing.T, api *mockContainer.MockAPIClient, fixture *dockerContainerFixture) {
	t.Helper()

	call := api.EXPECT().ContainerInspect(mock.Anything, string(fixture.id), dockerClient.ContainerInspectOptions{})
	if fixture.missing {
		call.Return(dockerClient.ContainerInspectResult{}, notFoundError("No such container: "+string(fixture.id))).Once()

		return
	}

	file := fixture.file
	if file == "" {
		file = fixture.name
	}

	info := readFixture[dockerContainer.InspectResponse](t, "container_"+file+".json")
	call.Return(dockerClient.ContainerInspectResult{Container: info}, nil).Once()
}

// expectForceRemove expects the target container to be force-removed with a
// context that is not canceled, so cleanup survives a canceled parent.
//
// Parameters:
//   - api: Mocked Docker API.
func expectForceRemove(api *mockContainer.MockAPIClient) {
	api.EXPECT().
		ContainerRemove(
			mock.MatchedBy(func(ctx context.Context) bool { return ctx.Err() == nil }),
			testTargetID,
			dockerClient.ContainerRemoveOptions{Force: true},
		).
		Return(dockerClient.ContainerRemoveResult{}, nil).Once()
}

// newRunningSource returns a running source container on the bridge
// network with a MAC address and a memory swappiness setting.
func newRunningSource() *container.Container {
	var defaultMem int64 = 60

	return modelfixture.Container(
		containerfixture.WithNetworkMode("bridge"),
		containerfixture.WithContainerState(dockerContainer.State{Running: true, Status: "running"}),
		containerfixture.WithNetworkSettings(map[string]*dockerNetwork.EndpointSettings{
			"bridge": {
				NetworkID:  "network_bridge_id",
				IPAddress:  netip.MustParseAddr("172.17.0.2"),
				MacAddress: dockerNetwork.HardwareAddr("aa:bb:cc:dd:ee:ff"),
				Aliases:    []string{"test-watchtower"},
			},
		}),
		func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
			if c.HostConfig == nil {
				c.HostConfig = &dockerContainer.HostConfig{}
			}

			c.HostConfig.MemorySwappiness = &defaultMem
		},
	)
}

func copyFromPath(filePath string) any {
	return mock.MatchedBy(func(options dockerClient.CopyFromContainerOptions) bool {
		return options.SourcePath == filePath
	})
}

// expectDaemonInfo stubs the single daemon info call made by one injection.
func expectDaemonInfo(api *mockContainer.MockAPIClient, securityOptions ...string) {
	api.EXPECT().
		Info(mock.Anything, dockerClient.InfoOptions{}).
		Return(dockerClient.SystemInfoResult{
			Info: dockerSystem.Info{SecurityOptions: securityOptions},
		}, nil).
		Once()
}

func mustFileTar(t *testing.T, name string, body []byte) []byte {
	t.Helper()

	return mustFileTarWithMode(t, name, body, 0o444)
}

// expectInfo expects one daemon info request and answers it with info.
//
// Parameters:
//   - api: Mocked Docker API.
//   - info: Daemon information to answer with.
func expectInfo(api *mockContainer.MockAPIClient, info dockerSystem.Info) {
	api.EXPECT().Info(mock.Anything, dockerClient.InfoOptions{}).
		Return(dockerClient.SystemInfoResult{Info: info}, nil).Once()
}

// expectServerVersion expects one server version request and answers it with
// the given components.
//
// Parameters:
//   - api: Mocked Docker API.
//   - components: Version components the daemon reports.
func expectServerVersion(api *mockContainer.MockAPIClient, components ...dockerSystem.ComponentVersion) {
	api.EXPECT().ServerVersion(mock.Anything, dockerClient.ServerVersionOptions{}).
		Return(dockerClient.ServerVersionResult{Components: components}, nil).Once()
}

// newImageAPI returns a mock Docker API whose daemon reports no registry mirrors.
//
// Parameters:
//   - t: Test that owns the mock.
//
// Returns:
//   - *mockContainer.MockAPIClient: Mock answering Info with an empty daemon configuration.
func newImageAPI(t *testing.T) *mockContainer.MockAPIClient {
	t.Helper()

	api := mockContainer.NewMockAPIClient(t)
	api.EXPECT().Info(mock.Anything, mock.Anything).Return(dockerClient.SystemInfoResult{}, nil).Maybe()

	return api
}

// runningImage returns a container that runs test-image:latest with the image
// ID imageID.
//
// Parameters:
//   - imageID: ID of the container's image.
//
// Returns:
//   - *container.Container: The container.
func runningImage(imageID string) *container.Container {
	return modelfixture.Container(
		containerfixture.WithImageName("test-image:latest"),
		func(container *dockerContainer.InspectResponse, image *dockerImage.InspectResponse) {
			container.Image = imageID
			image.ID = imageID
		},
	)
}

// inspected returns the inspect result of an image with id and repoDigests.
//
// Parameters:
//   - id: Image ID.
//   - repoDigests: The image's repo digests.
//
// Returns:
//   - dockerClient.ImageInspectResult: The inspect result.
func inspected(id string, repoDigests ...string) dockerClient.ImageInspectResult {
	return dockerClient.ImageInspectResult{ID: id, RepoDigests: repoDigests}
}
