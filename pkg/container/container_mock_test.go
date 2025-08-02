package container

import (
	"context"
	"fmt"
	"net/http"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerImageType "github.com/docker/docker/api/types/image"
	dockerMountType "github.com/docker/docker/api/types/mount"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerNat "github.com/docker/go-connections/nat"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/nicholas-fedor/watchtower/pkg/container/mocks"
)

// MockContainerUpdate defines a function to update mock container or image metadata for testing.
type MockContainerUpdate func(*dockerContainerType.InspectResponse, *dockerImageType.InspectResponse)

// MockContainer creates a mock Container instance with customizable metadata.
//
// Parameters:
//   - updates: Optional functions to modify container or image metadata.
//
// Returns:
//   - *Container: A configured mock container instance.
func MockContainer(updates ...MockContainerUpdate) *Container {
	// Initialize default container metadata with running state.
	containerInfo := dockerContainerType.InspectResponse{
		ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
			ID:         "container_id",
			Image:      "image",
			Name:       "test-watchtower",
			HostConfig: &dockerContainerType.HostConfig{},
			State: &dockerContainerType.State{
				Running: true,
				Status:  "running",
			},
		},
		Config: &dockerContainerType.Config{
			Labels: map[string]string{},
		},
		NetworkSettings: &dockerContainerType.NetworkSettings{
			Networks: map[string]*dockerNetworkType.EndpointSettings{},
		},
	}
	// Initialize default image metadata.
	image := dockerImageType.InspectResponse{
		ID:     "image_id",
		Config: &dockerspec.DockerOCIImageConfig{},
	}

	// Apply provided updates to container or image metadata.
	for _, update := range updates {
		update(&containerInfo, &image)
	}

	// Create and return a new Container instance.
	return NewContainer(&containerInfo, &image)
}

// WithPortBindings configures port bindings for the mock container.
//
// Parameters:
//   - portBindingSources: List of port binding sources (e.g., "80/tcp").
//
// Returns:
//   - MockContainerUpdate: Function to apply port bindings to container metadata.
func WithPortBindings(portBindingSources ...string) MockContainerUpdate {
	return func(container *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		portBindings := dockerNat.PortMap{}
		for _, pbs := range portBindingSources {
			portBindings[dockerNat.Port(pbs)] = []dockerNat.PortBinding{}
		}

		container.HostConfig.PortBindings = portBindings
	}
}

// WithImageName sets the image name for the mock container and image.
//
// Parameters:
//   - name: Image name to set (e.g., "test-image:latest").
//
// Returns:
//   - MockContainerUpdate: Function to set image name in container and image metadata.
func WithImageName(name string) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, i *dockerImageType.InspectResponse) {
		c.Config.Image = name
		i.RepoTags = append(i.RepoTags, name)
	}
}

// WithLinks sets dependency links for the mock container.
//
// Parameters:
//   - links: List of links in "name:alias" format.
//
// Returns:
//   - MockContainerUpdate: Function to set links in container HostConfig.
func WithLinks(links []string) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		c.HostConfig.Links = links
	}
}

// WithLabels sets labels for the mock container.
//
// Parameters:
//   - labels: Map of label key-value pairs.
//
// Returns:
//   - MockContainerUpdate: Function to set labels in container Config.
func WithLabels(labels map[string]string) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		c.Config.Labels = labels
	}
}

// WithContainerState sets the state for the mock container.
//
// Parameters:
//   - state: Container state (e.g., running, stopped).
//
// Returns:
//   - MockContainerUpdate: Function to set state in container metadata.
func WithContainerState(state dockerContainerType.State) MockContainerUpdate {
	return func(cnt *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		cnt.State = &state
	}
}

// WithHealthcheck sets the healthcheck configuration for the mock container.
//
// Parameters:
//   - healthConfig: Healthcheck configuration to set.
//
// Returns:
//   - MockContainerUpdate: Function to set healthcheck in container Config.
func WithHealthcheck(healthConfig dockerContainerType.HealthConfig) MockContainerUpdate {
	return func(cnt *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		cnt.Config.Healthcheck = &healthConfig
	}
}

// WithImageHealthcheck sets the healthcheck configuration for the mock image.
//
// Parameters:
//   - healthConfig: Healthcheck configuration to set.
//
// Returns:
//   - MockContainerUpdate: Function to set healthcheck in image Config.
func WithImageHealthcheck(healthConfig dockerContainerType.HealthConfig) MockContainerUpdate {
	return func(_ *dockerContainerType.InspectResponse, img *dockerImageType.InspectResponse) {
		img.Config.Healthcheck = &healthConfig
	}
}

// WithNetworkMode sets the network mode for the mock container.
//
// Parameters:
//   - mode: Network mode (e.g., "bridge", "host").
//
// Returns:
//   - MockContainerUpdate: Function to set network mode in container HostConfig.
func WithNetworkMode(mode string) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainerType.HostConfig{}
		}

		c.HostConfig.NetworkMode = dockerContainerType.NetworkMode(mode)
		logrus.WithFields(logrus.Fields{
			"mode":    mode,
			"is_host": mode == "host",
		}).Debug("MockContainer set NetworkMode")
	}
}

// WithNetworkSettings sets network settings for the mock container.
//
// Parameters:
//   - networks: Map of network names to endpoint settings.
//
// Returns:
//   - MockContainerUpdate: Function to set network settings in container NetworkSettings.
func WithNetworkSettings(
	networks map[string]*dockerNetworkType.EndpointSettings,
) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		if c.NetworkSettings == nil {
			c.NetworkSettings = &dockerContainerType.NetworkSettings{}
		}

		c.NetworkSettings.Networks = networks
	}
}

// WithMounts sets mounts for the mock container.
//
// Parameters:
//   - mounts: List of mount configurations.
//
// Returns:
//   - MockContainerUpdate: Function to set mounts in container HostConfig.
func WithMounts(mounts []dockerMountType.Mount) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		c.HostConfig.Mounts = mounts
	}
}

// WithNetworks adds multiple networks to the mock container.
//
// Parameters:
//   - networkNames: List of network names to add.
//
// Returns:
//   - MockContainerUpdate: Function to add networks to container NetworkSettings.
func WithNetworks(networkNames ...string) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		if c.NetworkSettings == nil {
			c.NetworkSettings = &dockerContainerType.NetworkSettings{}
		}

		if c.NetworkSettings.Networks == nil {
			c.NetworkSettings.Networks = make(map[string]*dockerNetworkType.EndpointSettings)
		}

		for _, name := range networkNames {
			c.NetworkSettings.Networks[name] = &dockerNetworkType.EndpointSettings{
				NetworkID: fmt.Sprintf("network_%s_id", name),
				Aliases:   []string{c.Name},
			}
			logrus.WithFields(logrus.Fields{
				"container": c.Name,
				"network":   name,
			}).Debug("MockContainer added network")
		}
	}
}

// MockClient is a mock implementation of the Operations interface for testing container operations.
type MockClient struct {
	createFunc  func(context.Context, *dockerContainerType.Config, *dockerContainerType.HostConfig, *dockerNetworkType.NetworkingConfig, *ocispec.Platform, string) (dockerContainerType.CreateResponse, error)
	startFunc   func(context.Context, string, dockerContainerType.StartOptions) error
	removeFunc  func(context.Context, string, dockerContainerType.RemoveOptions) error
	connectFunc func(context.Context, string, string, *dockerNetworkType.EndpointSettings) error
	renameFunc  func(context.Context, string, string) error
}

// ContainerCreate mocks the creation of a new container.
//
// Parameters:
//   - ctx: Context for the operation.
//   - config: Container configuration.
//   - hostConfig: Host configuration.
//   - networkingConfig: Network configuration.
//   - platform: Platform specification.
//   - containerName: Name for the new container.
//
// Returns:
//   - dockerContainerType.CreateResponse: Mocked response with container ID.
//   - error: Error if the mock create function is set to fail, nil otherwise.
func (m *MockClient) ContainerCreate(
	ctx context.Context,
	config *dockerContainerType.Config,
	hostConfig *dockerContainerType.HostConfig,
	networkingConfig *dockerNetworkType.NetworkingConfig,
	platform *ocispec.Platform,
	containerName string,
) (dockerContainerType.CreateResponse, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, config, hostConfig, networkingConfig, platform, containerName)
	}

	return dockerContainerType.CreateResponse{ID: "new_container_id"}, nil
}

// ContainerStart mocks the start of a container.
//
// Parameters:
//   - ctx: Context for the operation.
//   - containerID: ID of the container to start.
//   - options: Start options.
//
// Returns:
//   - error: Error if the mock start function is set to fail, nil otherwise.
func (m *MockClient) ContainerStart(
	ctx context.Context,
	containerID string,
	options dockerContainerType.StartOptions,
) error {
	if m.startFunc != nil {
		return m.startFunc(ctx, containerID, options)
	}

	return nil
}

// ContainerRemove mocks the removal of a container.
//
// Parameters:
//   - ctx: Context for the operation.
//   - containerID: ID of the container to remove.
//   - options: Removal options (e.g., force, remove volumes).
//
// Returns:
//   - error: Error if the mock remove function is set to fail, nil otherwise.
func (m *MockClient) ContainerRemove(
	ctx context.Context,
	containerID string,
	options dockerContainerType.RemoveOptions,
) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, containerID, options)
	}

	return nil
}

// NetworkConnect mocks connecting a container to a network.
//
// Parameters:
//   - ctx: Context for the operation.
//   - networkID: ID of the network.
//   - containerID: ID of the container.
//   - config: Endpoint settings for the network connection.
//
// Returns:
//   - error: Error if the mock connect function is set to fail, nil otherwise.
func (m *MockClient) NetworkConnect(
	ctx context.Context,
	networkID, containerID string,
	config *dockerNetworkType.EndpointSettings,
) error {
	if m.connectFunc != nil {
		return m.connectFunc(ctx, networkID, containerID, config)
	}

	return nil
}

// ContainerRename mocks renaming a container.
//
// Parameters:
//   - ctx: Context for the operation.
//   - containerID: ID of the container to rename.
//   - newContainerName: New name for the container.
//
// Returns:
//   - error: Error if the mock rename function is set to fail, nil otherwise.
func (m *MockClient) ContainerRename(
	ctx context.Context,
	containerID, newContainerName string,
) error {
	if m.renameFunc != nil {
		return m.renameFunc(ctx, containerID, newContainerName)
	}

	return nil
}

func StopContainerHandler(containerID string, status mocks.FoundStatus) http.HandlerFunc {
	return ghttp.CombineHandlers(
		ghttp.VerifyRequest(
			"POST",
			gomega.HaveSuffix(fmt.Sprintf("containers/%s/stop", containerID)),
		),
		func(w http.ResponseWriter, r *http.Request) {
			code := 404
			if status {
				code = 204
			}

			ghttp.RespondWith(code, nil)(w, r)
		},
	)
}
