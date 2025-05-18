package container

import (
	"github.com/sirupsen/logrus"

	dockerContainerType "github.com/docker/docker/api/types/container"
	dockerImageType "github.com/docker/docker/api/types/image"
	dockerMountType "github.com/docker/docker/api/types/mount"
	dockerNetworkType "github.com/docker/docker/api/types/network"
	dockerNat "github.com/docker/go-connections/nat"
)

type MockContainerUpdate func(*dockerContainerType.InspectResponse, *dockerImageType.InspectResponse)

func MockContainer(updates ...MockContainerUpdate) *Container {
	containerInfo := dockerContainerType.InspectResponse{
		ContainerJSONBase: &dockerContainerType.ContainerJSONBase{
			ID:         "container_id",
			Image:      "image",
			Name:       "test-watchtower",
			HostConfig: &dockerContainerType.HostConfig{},
		},
		Config: &dockerContainerType.Config{
			Labels: map[string]string{},
		},
	}
	image := dockerImageType.InspectResponse{
		ID:     "image_id",
		Config: &dockerContainerType.Config{},
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
