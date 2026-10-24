package containerfixture

import (
	"fmt"

	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerMount "github.com/moby/moby/api/types/mount"
	dockerNetwork "github.com/moby/moby/api/types/network"
)

// Update changes the container or image inspect data that Inspect builds.
type Update func(*dockerContainer.InspectResponse, *dockerImage.InspectResponse)

// Inspect builds the inspect data of a running container and its image, then
// applies updates to it in order.
//
// Parameters:
//   - updates: Changes to the container or image inspect data.
//
// Returns:
//   - *dockerContainer.InspectResponse: Container inspect data.
//   - *dockerImage.InspectResponse: Image inspect data.
func Inspect(updates ...Update) (*dockerContainer.InspectResponse, *dockerImage.InspectResponse) {
	// Initialize default container metadata with running state.
	containerInfo := dockerContainer.InspectResponse{
		ID:         "container_id",
		Image:      "image",
		Name:       "test-watchtower",
		HostConfig: &dockerContainer.HostConfig{},
		State: &dockerContainer.State{
			Running: true,
			Status:  "running",
		},
		Config: &dockerContainer.Config{
			Labels: map[string]string{},
		},
		NetworkSettings: &dockerContainer.NetworkSettings{
			Networks: map[string]*dockerNetwork.EndpointSettings{},
		},
	}
	// Initialize default image metadata.
	image := dockerImage.InspectResponse{
		ID:     "image_id",
		Config: &dockerspec.DockerOCIImageConfig{},
	}

	// Apply provided updates to container or image metadata.
	for _, update := range updates {
		update(&containerInfo, &image)
	}

	return &containerInfo, &image
}

// WithPortBindings configures port bindings for the mock container.
//
// Parameters:
//   - portBindingSources: List of port binding sources (e.g., "80/tcp").
//
// Returns:
//   - Update: Function to apply port bindings to container metadata.
func WithPortBindings(portBindingSources ...string) Update {
	return func(container *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		portBindings := dockerNetwork.PortMap{}

		for _, pbs := range portBindingSources {
			port, err := dockerNetwork.ParsePort(pbs)
			if err != nil {
				panic(fmt.Sprintf("failed to parse port %s: %v", pbs, err))
			}

			portBindings[port] = []dockerNetwork.PortBinding{}
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
//   - Update: Function to set image name in container and image metadata.
func WithImageName(name string) Update {
	return func(c *dockerContainer.InspectResponse, i *dockerImage.InspectResponse) {
		c.Config.Image = name
		i.RepoTags = append(i.RepoTags, name)
	}
}

// WithRepoDigests sets the RepoDigests for the mock image.
//
// Parameters:
//   - digests: List of repo digests (e.g., ["repo@sha256:abc"]).
//
// Returns:
//   - Update: Function to set RepoDigests on image metadata.
func WithRepoDigests(digests []string) Update {
	return func(_ *dockerContainer.InspectResponse, i *dockerImage.InspectResponse) {
		i.RepoDigests = digests
	}
}

// WithLinks sets dependency links for the mock container.
//
// Parameters:
//   - links: List of links in "name:alias" format.
//
// Returns:
//   - Update: Function to set links in container HostConfig.
func WithLinks(links []string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		c.HostConfig.Links = links
	}
}

// WithVolumesFrom sets HostConfig.VolumesFrom for the mock container.
//
// Parameters:
//   - volumesFrom: List of volumes-from specs (name or ID, optional :ro/:rw).
//
// Returns:
//   - Update: Function to set VolumesFrom in container HostConfig.
func WithVolumesFrom(volumesFrom []string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.VolumesFrom = volumesFrom
	}
}

// WithLabels sets labels for the mock container.
//
// Parameters:
//   - labels: Map of label key-value pairs.
//
// Returns:
//   - Update: Function to set labels in container Config.
func WithLabels(labels map[string]string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		c.Config.Labels = labels
	}
}

// WithImageLabels sets labels on the mock image config (OCI annotations).
func WithImageLabels(labels map[string]string) Update {
	return func(_ *dockerContainer.InspectResponse, i *dockerImage.InspectResponse) {
		if i.Config == nil {
			i.Config = &dockerspec.DockerOCIImageConfig{}
		}

		i.Config.Labels = labels
	}
}

// WithContainerState sets the state for the mock container.
//
// Each Inspect gets its own copy of state, so containers built from one Update
// do not share it.
//
// Parameters:
//   - state: Container state (e.g., running, stopped).
//
// Returns:
//   - Update: Function to set state in container metadata.
func WithContainerState(state dockerContainer.State) Update {
	return func(cnt *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		stateCopy := state
		cnt.State = &stateCopy
	}
}

// WithHealthcheck sets the healthcheck configuration for the mock container.
//
// Each Inspect gets its own copy of healthConfig, so containers built from one
// Update do not share it.
//
// Parameters:
//   - healthConfig: Healthcheck configuration to set.
//
// Returns:
//   - Update: Function to set healthcheck in container Config.
func WithHealthcheck(healthConfig dockerContainer.HealthConfig) Update {
	return func(cnt *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		healthCopy := healthConfig
		cnt.Config.Healthcheck = &healthCopy
	}
}

// WithImageHealthcheck sets the healthcheck configuration for the mock image.
//
// Each Inspect gets its own copy of healthConfig, so images built from one
// Update do not share it.
//
// Parameters:
//   - healthConfig: Healthcheck configuration to set.
//
// Returns:
//   - Update: Function to set healthcheck in image Config.
func WithImageHealthcheck(healthConfig dockerContainer.HealthConfig) Update {
	return func(_ *dockerContainer.InspectResponse, img *dockerImage.InspectResponse) {
		healthCopy := healthConfig
		img.Config.Healthcheck = &healthCopy
	}
}

// WithNetworkMode sets the network mode for the mock container.
//
// Parameters:
//   - mode: Network mode (e.g., "bridge", "host").
//
// Returns:
//   - Update: Function to set network mode in container HostConfig.
func WithNetworkMode(mode string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.NetworkMode = dockerContainer.NetworkMode(mode)
	}
}

// WithNetworkSettings sets network settings for the mock container.
//
// Parameters:
//   - networks: Map of network names to endpoint settings.
//
// Returns:
//   - Update: Function to set network settings in container NetworkSettings.
func WithNetworkSettings(
	networks map[string]*dockerNetwork.EndpointSettings,
) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.NetworkSettings == nil {
			c.NetworkSettings = &dockerContainer.NetworkSettings{}
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
//   - Update: Function to set mounts in container HostConfig.
func WithMounts(mounts []dockerMount.Mount) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		c.HostConfig.Mounts = mounts
	}
}

// WithInspectMounts sets runtime mount points on the mock container inspect response.
//
// Parameters:
//   - mounts: Runtime mount points reported by container inspect.
//
// Returns:
//   - Update: Function to set inspect Mounts.
func WithInspectMounts(mounts []dockerContainer.MountPoint) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		c.Mounts = mounts
	}
}

// WithReadonlyRootfs sets HostConfig.ReadonlyRootfs on the mock container.
//
// Parameters:
//   - readOnly: Whether the container root filesystem is read-only.
//
// Returns:
//   - Update: Function to set ReadonlyRootfs.
func WithReadonlyRootfs(readOnly bool) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.ReadonlyRootfs = readOnly
	}
}

// WithNetworks adds multiple networks to the mock container.
//
// Parameters:
//   - networkNames: List of network names to add.
//
// Returns:
//   - Update: Function to add networks to container NetworkSettings.
func WithNetworks(networkNames ...string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.NetworkSettings == nil {
			c.NetworkSettings = &dockerContainer.NetworkSettings{}
		}

		if c.NetworkSettings.Networks == nil {
			c.NetworkSettings.Networks = make(map[string]*dockerNetwork.EndpointSettings)
		}

		for _, name := range networkNames {
			c.NetworkSettings.Networks[name] = &dockerNetwork.EndpointSettings{
				NetworkID: fmt.Sprintf("network_%s_id", name),
				Aliases:   []string{c.Name},
			}
		}
	}
}

// WithUTSMode sets the UTS mode for the mock container.
//
// Parameters:
//   - mode: UTS mode to set (e.g., "host").
//
// Returns:
//   - Update: Function to set UTS mode in container HostConfig.
func WithUTSMode(mode string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.UTSMode = dockerContainer.UTSMode(mode)
	}
}

// WithHostname sets the hostname for the mock container.
//
// Parameters:
//   - hostname: Hostname to set.
//
// Returns:
//   - Update: Function to set hostname in container Config.
func WithHostname(hostname string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.Config == nil {
			c.Config = &dockerContainer.Config{}
		}

		c.Config.Hostname = hostname
	}
}

// WithAutoRemove sets the AutoRemove flag for the mock container.
//
// Parameters:
//   - autoRemove: Whether to auto-remove the container on stop.
//
// Returns:
//   - Update: Function to set AutoRemove in container HostConfig.
func WithAutoRemove(autoRemove bool) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.AutoRemove = autoRemove
	}
}

// WithName sets the container name for the mock container.
//
// Parameters:
//   - name: Container name to set (e.g., "my-app").
//
// Returns:
//   - Update: Function to set the container name in metadata.
func WithName(name string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		c.Name = name
	}
}

// WithID sets the container ID for the mock container.
//
// Parameters:
//   - id: Container ID to set (e.g., "abc123").
//
// Returns:
//   - Update: Function to set the container ID in metadata.
func WithID(id string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		c.ID = id
	}
}

// WithEnv sets environment variables on the mock container's Config.
//
// Parameters:
//   - env: Environment variables in "KEY=VALUE" format.
//
// Returns:
//   - Update: Function to set environment variables in container Config.
func WithEnv(env []string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.Config == nil {
			c.Config = &dockerContainer.Config{}
		}

		c.Config.Env = env
	}
}

// WithBinds sets bind mounts on the mock container's HostConfig.
//
// Parameters:
//   - binds: Bind mount strings in "host_path:container_path" format.
//
// Returns:
//   - Update: Function to set bind mounts in container HostConfig.
func WithBinds(binds []string) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.Binds = binds
	}
}

// WithDevices sets device mappings on the mock container's HostConfig.
//
// This is primarily used to test device permission normalization (empty
// CgroupPermissions defaulting to "rwm") for Podman compatibility.
//
// Parameters:
//   - devices: Slice of DeviceMapping structs.
//
// Returns:
//   - Update: Function to set devices in container HostConfig.
func WithDevices(devices []dockerContainer.DeviceMapping) Update {
	return func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
		if c.HostConfig == nil {
			c.HostConfig = &dockerContainer.HostConfig{}
		}

		c.HostConfig.Devices = devices
	}
}
