package container

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerImageType "github.com/docker/docker/api/types/image"
	dockerMountType "github.com/docker/docker/api/types/mount"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerNat "github.com/docker/go-connections/nat"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type MockContainerUpdate func(*dockerContainerType.InspectResponse, *dockerImageType.InspectResponse)

func MockContainer(updates ...MockContainerUpdate) *Container {
	containerInfo := dockerContainerType.InspectResponse{
		ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
			ID:         "container_id",
			Image:      "image",
			Name:       "test-watchtower",
			HostConfig: &dockerContainerType.HostConfig{},
			State: &dockerContainerType.State{
				Running: true,
				Status:  "running",
			}, // Default to running
		},
		Config: &dockerContainerType.Config{
			Labels: map[string]string{},
		},
		NetworkSettings: &dockerContainerType.NetworkSettings{
			Networks: map[string]*dockerNetworkType.EndpointSettings{},
		},
	}
	image := dockerImageType.InspectResponse{
		ID:     "image_id",
		Config: &dockerspec.DockerOCIImageConfig{},
	}

	for _, update := range updates {
		update(&containerInfo, &image)
	}

	return NewContainer(&containerInfo, &image)
}

func WithPortBindings(portBindingSources ...string) MockContainerUpdate {
	return func(container *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		portBindings := dockerNat.PortMap{}
		for _, pbs := range portBindingSources {
			portBindings[dockerNat.Port(pbs)] = []dockerNat.PortBinding{}
		}

		container.HostConfig.PortBindings = portBindings
	}
}

func WithImageName(name string) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, i *dockerImageType.InspectResponse) {
		c.Config.Image = name
		i.RepoTags = append(i.RepoTags, name)
	}
}

func WithLinks(links []string) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		c.HostConfig.Links = links
	}
}

func WithLabels(labels map[string]string) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		c.Config.Labels = labels
	}
}

func WithContainerState(state dockerContainerType.State) MockContainerUpdate {
	return func(cnt *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		cnt.State = &state
	}
}

func WithHealthcheck(healthConfig dockerContainerType.HealthConfig) MockContainerUpdate {
	return func(cnt *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		cnt.Config.Healthcheck = &healthConfig
	}
}

func WithImageHealthcheck(healthConfig dockerContainerType.HealthConfig) MockContainerUpdate {
	return func(_ *dockerContainerType.InspectResponse, img *dockerImageType.InspectResponse) {
		img.Config.Healthcheck = &healthConfig
	}
}

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

func WithMounts(mounts []dockerMountType.Mount) MockContainerUpdate {
	return func(c *dockerContainerType.InspectResponse, _ *dockerImageType.InspectResponse) {
		c.HostConfig.Mounts = mounts
	}
}

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

func (m *MockClient) ContainerRename(
	ctx context.Context,
	containerID, newContainerName string,
) error {
	if m.renameFunc != nil {
		return m.renameFunc(ctx, containerID, newContainerName)
	}

	return nil
}
