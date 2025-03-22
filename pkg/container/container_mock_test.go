package container

import (
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"
)

type MockContainerUpdate func(*container.InspectResponse, *image.InspectResponse)

//nolint:exhaustruct // Mock structs intentionally omit fields irrelevant to tests
func MockContainer(updates ...MockContainerUpdate) *Container {
	containerInfo := container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:         "container_id",
			Image:      "image",
			Name:       "test-watchtower",
			HostConfig: &container.HostConfig{},
		},
		Config: &container.Config{
			Labels: map[string]string{},
		},
	}
	image := image.InspectResponse{
		ID:     "image_id",
		Config: &container.Config{},
	}

	for _, update := range updates {
		update(&containerInfo, &image)
	}

	return NewContainer(&containerInfo, &image)
}

func WithPortBindings(portBindingSources ...string) MockContainerUpdate {
	return func(container *container.InspectResponse, _ *image.InspectResponse) {
		portBindings := nat.PortMap{}
		for _, pbs := range portBindingSources {
			portBindings[nat.Port(pbs)] = []nat.PortBinding{}
		}

		container.HostConfig.PortBindings = portBindings
	}
}

func WithImageName(name string) MockContainerUpdate {
	return func(c *container.InspectResponse, i *image.InspectResponse) {
		c.Config.Image = name
		i.RepoTags = append(i.RepoTags, name)
	}
}

func WithLinks(links []string) MockContainerUpdate {
	return func(c *container.InspectResponse, _ *image.InspectResponse) {
		c.HostConfig.Links = links
	}
}

func WithLabels(labels map[string]string) MockContainerUpdate {
	return func(c *container.InspectResponse, _ *image.InspectResponse) {
		c.Config.Labels = labels
	}
}

func WithContainerState(state container.State) MockContainerUpdate {
	return func(cnt *container.InspectResponse, _ *image.InspectResponse) {
		cnt.State = &state
	}
}

func WithHealthcheck(healthConfig container.HealthConfig) MockContainerUpdate {
	return func(cnt *container.InspectResponse, _ *image.InspectResponse) {
		cnt.Config.Healthcheck = &healthConfig
	}
}

func WithImageHealthcheck(healthConfig container.HealthConfig) MockContainerUpdate {
	return func(_ *container.InspectResponse, img *image.InspectResponse) {
		img.Config.Healthcheck = &healthConfig
	}
}
