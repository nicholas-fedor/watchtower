package recreate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/moby/moby/client/pkg/versions"
	"github.com/rs/zerolog"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"
	dockerMount "github.com/moby/moby/api/types/mount"
	dockerNetwork "github.com/moby/moby/api/types/network"
	dockerClient "github.com/moby/moby/client"

	composeLabels "github.com/nicholas-fedor/watchtower/internal/compose/labels"
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/network"
)

// keptVolume records an anonymous volume that a recreation mounted by name.
type keptVolume struct {
	// Destination is the volume's path in the container, as the container
	// declared it.
	Destination string `json:"destination"`
	// Volume is the name of the volume.
	Volume string `json:"volume"`
	// Form is how the container declared the volume: as a bind of the
	// destination alone, as a volume mount without a source, or neither.
	Form string `json:"form"`
	// NoCopy is the mount's own no-copy setting, for a volume mount.
	NoCopy bool `json:"noCopy,omitempty"`
}

const (
	// CleanupTimeout is the timeout for cleanup operations (e.g., container remove).
	// Using a dedicated timeout ensures cleanup completes even if the parent context is canceled.
	CleanupTimeout = 10 * time.Second

	// manifestsMinAPIVersion is the first Docker API version whose image
	// inspect reports the image's manifests.
	manifestsMinAPIVersion = "1.48"
)

// How a container declared an anonymous volume that a recreation kept.
const (
	// keptAsBind is a bind of the destination alone.
	keptAsBind = "bind"
	// keptAsMount is a volume mount without a source.
	keptAsMount = "mount"
	// keptAsVolume is a volume the container has without a bind or mount,
	// such as one its image declares.
	keptAsVolume = "volume"
)

// Errors for creating and renaming replacement containers.
var (
	// errInspectImageFailed indicates a failure to inspect an image from the Docker daemon.
	errInspectImageFailed = errors.New("failed to inspect image")
	// errCreateContainerFailed indicates a failure to create a new container.
	errCreateContainerFailed = errors.New("failed to create container")
	// errRenameContainerFailed indicates a failure to give a created container the source container's name.
	errRenameContainerFailed = errors.New("failed to rename container")
)

// CreateTarget creates a new container based on the source container's
// configuration but does not start it. It applies the provided network configuration,
// renames the container, and attaches additional networks for legacy API versions.
// A container rebound to a recreated container's network keeps the image it
// runs and its anonymous volumes.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - sourceContainer: Source container to replicate.
//   - networkConfig: Network configuration to apply to the new container.
//   - clientVersion: Docker API version used by the client.
//   - disableMemorySwappiness: If true, disables memory swappiness for Podman compatibility.
//   - cpuCopyMode: CPU copy mode for container recreation, used for compatibility with Podman.
//   - isPodman: If true, indicates Podman is being used for CPU compatibility.
//
// Returns:
//   - container.ContainerID: ID of the new container.
//   - error: Non-nil if creation fails, nil on success.
func CreateTarget(log *zerolog.Logger,
	ctx context.Context,
	api dockerClient.APIClient,
	sourceContainer *container.Container,
	networkConfig *dockerNetwork.NetworkingConfig,
	clientVersion string,
	disableMemorySwappiness bool,
	cpuCopyMode string,
	isPodman bool,
) (container.ContainerID, error) {
	clogVal := log.With().
		Str("container", sourceContainer.Name()).
		Str("id", sourceContainer.ID().ShortID()).
		Logger()
	clog := &clogVal

	// Extract configuration from the source container.
	config := sourceContainer.GetCreateConfig()
	hostConfig := sourceContainer.GetCreateHostConfig()

	// A container recreated only to join a recreated container's network
	// namespace changes nothing else, so its Compose image label still names
	// its image. A rebound container with AutoRemove keeps the volumes an
	// earlier recreation mounted by name, since Docker does not remove those
	// with it.
	if sourceContainer.IsRebinding() {
		keepRunningImage(ctx, api, clog, sourceContainer, config)

		if sourceContainer.KeepsAnonymousVolumes() {
			keepAnonymousVolumes(clog, sourceContainer, config, hostConfig)
		}
	} else {
		syncComposeImageLabel(ctx, api, clog, sourceContainer, config, clientVersion)
		restoreAnonymousVolumes(clog, config, hostConfig)
	}

	// Set MemorySwappiness to nil for Podman compatibility if flag is enabled.
	if disableMemorySwappiness {
		hostConfig.MemorySwappiness = nil

		clog.Debug().Msg("Disabled memory swappiness for Podman compatibility")
	}

	// Handle CPU settings based on copy mode.
	handleCPUSettings(hostConfig, cpuCopyMode, isPodman, clog)

	// Determine network config for container creation based on API version.
	createNetworkConfig := networkConfig

	if versions.LessThan(clientVersion, "1.44") && len(networkConfig.EndpointsConfig) > 1 {
		// Legacy API (< 1.44) with multiple networks: use first network for creation.
		var firstNetworkName string

		createNetworkConfig = network.EmptyConfig()

		for name, endpoint := range networkConfig.EndpointsConfig {
			firstNetworkName = name
			createNetworkConfig.EndpointsConfig[name] = endpoint

			clog.Debug().
				Str("network", firstNetworkName).
				Msg("Selected first network for container creation")

			break // Use only the first network initially.
		}
	} else {
		clog.Debug().Msg("Using full network config for API version >= 1.44 or single network")
	}

	// Create container with the selected network config.
	clog.Debug().Msg("Creating new container")

	createdContainer, err := api.ContainerCreate(
		ctx,
		dockerClient.ContainerCreateOptions{
			Config:           config,
			HostConfig:       hostConfig,
			NetworkingConfig: createNetworkConfig,
			Name:             "",
		},
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to create new container")

		return "", fmt.Errorf("%w: %w", errCreateContainerFailed, err)
	}

	createdContainerID := container.ContainerID(createdContainer.ID)
	clog.Debug().
		Str("new_id", string(createdContainerID)).
		Msg("Created container successfully")

	// Rename the container to the correct name to avoid conflicts during self-update
	clog.Debug().Msg("Renaming container to correct name")

	_, err = api.ContainerRename(
		ctx,
		createdContainer.ID,
		dockerClient.ContainerRenameOptions{
			NewName: sourceContainer.Name(),
		},
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to rename container")

		// Clean up the created container to avoid orphaned resources
		cleanupCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			CleanupTimeout,
		)
		defer cancel()

		_, rmErr := api.ContainerRemove(
			cleanupCtx,
			createdContainer.ID,
			dockerClient.ContainerRemoveOptions{Force: true},
		)
		if rmErr != nil {
			clog.Warn().
				Err(rmErr).
				Msg("Failed to clean up container after rename error")
		}

		return "", fmt.Errorf("%w: %w", errRenameContainerFailed, err)
	}

	// Attach additional networks for legacy API if needed.
	if versions.LessThan(clientVersion, "1.44") && len(networkConfig.EndpointsConfig) > 1 {
		err := attachNetworks(
			ctx,
			api,
			createdContainer.ID,
			networkConfig,
			createNetworkConfig,
			clog,
		)
		if err != nil {
			// Clean up the created container to avoid orphaned resources.
			cleanupCtx, cancel := context.WithTimeout(
				context.WithoutCancel(ctx),
				CleanupTimeout,
			)
			defer cancel()

			_, rmErr := api.ContainerRemove(
				cleanupCtx,
				createdContainer.ID,
				dockerClient.ContainerRemoveOptions{Force: true},
			)
			if rmErr != nil {
				clog.Warn().
					Err(rmErr).
					Msg("Failed to clean up container after network attachment error")
			}

			return "", err
		}
	}

	return createdContainerID, nil
}

// keepRunningImage creates the new container from the image the source
// container runs.
//
// When the image reference the source container was created from now names
// another image or none, config is changed to use the running image's ID.
// The reference is then kept in the Zodiac original-image label, so the new
// container still reports it as its image and is updated from it.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - clog: Logger with container context fields.
//   - sourceContainer: Source container the new container replaces.
//   - config: Creation config for the new container, updated in place.
func keepRunningImage(
	ctx context.Context,
	api dockerClient.APIClient,
	clog *zerolog.Logger,
	sourceContainer *container.Container,
	config *dockerContainer.Config,
) {
	// The container inspect names the running image even when the image's
	// own inspect failed.
	info := sourceContainer.ContainerInfo()
	if info == nil || info.Image == "" || config.Image == info.Image {
		return
	}

	named, err := api.ImageInspect(ctx, config.Image)
	if err == nil && named.ID == info.Image {
		return
	}

	clog.Debug().
		Err(err).
		Str("image", config.Image).
		Str("image_id", info.Image).
		Msg("Image reference no longer names the running image. Recreating the rebound container from its image ID")

	if _, ok := config.Labels[labels.ZodiacOriginalImage]; !ok {
		if config.Labels == nil {
			config.Labels = make(map[string]string, 1)
		}

		config.Labels[labels.ZodiacOriginalImage] = config.Image
	}

	config.Image = info.Image
}

// keepAnonymousVolumes mounts the source container's anonymous volumes in the
// new container.
//
// A new container otherwise gets new, empty anonymous volumes at those paths.
// A bind that gives only a destination is an anonymous volume and names the
// volume instead. Other binds and named volume mounts in hostConfig are left
// as they are. So is a volume at a destination that a volumes-from container
// mounted when the source container was listed, which Docker takes from that
// container when it creates the new one. Each kept volume is mounted without
// copying the image's files into it, so a volume the container emptied stays
// empty. The new container's labels record the kept volumes, together with
// those an earlier recreation kept, so a later recreation that does not keep
// them mounts them anonymously again.
//
// Parameters:
//   - clog: Logger with container context fields.
//   - sourceContainer: Source container the new container replaces.
//   - config: Config for the new container, whose labels are updated in
//     place.
//   - hostConfig: Host config for the new container, updated in place.
func keepAnonymousVolumes(
	clog *zerolog.Logger,
	sourceContainer *container.Container,
	config *dockerContainer.Config,
	hostConfig *dockerContainer.HostConfig,
) {
	info := sourceContainer.ContainerInfo()
	if info == nil {
		return
	}

	record := readKeptVolumes(clog, config)
	earlier := len(record)

	// Docker names a volume for every volume mount point, including the
	// anonymous ones. It cleans mount point destinations but keeps binds and
	// mount targets as written, such as /data/, so every path is compared
	// cleaned.
	volumeAt := make(map[string]string, len(info.Mounts))

	for _, point := range info.Mounts {
		if point.Type == dockerMount.TypeVolume && point.Name != "" {
			volumeAt[path.Clean(point.Destination)] = point.Name
		}
	}

	// The source container's inspect also lists the volumes it got from its
	// volumes-from containers. Mounting one by name would keep it in place of
	// the volume such a container has after it is recreated. Their
	// destinations come from the listing, since a volumes-from container
	// recreated before this one may mount other paths now.
	fromOthers := sourceContainer.VolumesFromDestinations()

	declared := make(map[string]bool, len(hostConfig.Binds)+len(hostConfig.Mounts)+len(fromOthers))
	for _, destination := range fromOthers {
		declared[destination] = true
	}

	for i, bind := range hostConfig.Binds {
		if parsed, ok := container.ParseBind(bind); ok {
			declared[path.Clean(parsed.Destination)] = true

			continue
		}

		// Docker reads a bind of a destination alone as an anonymous volume
		// there, so the bind names the volume the container has.
		name := volumeAt[path.Clean(bind)]
		if strings.Contains(bind, ":") || name == "" {
			continue
		}

		declared[path.Clean(bind)] = true
		hostConfig.Binds[i] = name + ":" + bind + ":nocopy"
		record = append(record, keptVolume{Destination: bind, Volume: name, Form: keptAsBind, NoCopy: false})
	}

	for i, declaredMount := range hostConfig.Mounts {
		target := path.Clean(declaredMount.Target)
		declared[target] = true

		// A volume mount without a source is anonymous. Podman ignores
		// NoCopy on a mount, so it may still copy the image's files into a
		// volume that is empty.
		if declaredMount.Type == dockerMount.TypeVolume && declaredMount.Source == "" && volumeAt[target] != "" {
			options := volumeOptions(declaredMount)
			record = append(record, keptVolume{
				Destination: declaredMount.Target,
				Volume:      volumeAt[target],
				Form:        keptAsMount,
				NoCopy:      options.NoCopy,
			})

			options.NoCopy = true
			hostConfig.Mounts[i].Source = volumeAt[target]
			hostConfig.Mounts[i].VolumeOptions = &options
		}
	}

	// The remaining volumes are added as binds, whose nocopy mode Docker and
	// Podman both honor.
	for _, point := range info.Mounts {
		if point.Type != dockerMount.TypeVolume || point.Name == "" || declared[path.Clean(point.Destination)] {
			continue
		}

		mode := "nocopy"
		if !point.RW {
			mode = "ro,nocopy"
		}

		hostConfig.Binds = append(hostConfig.Binds, point.Name+":"+point.Destination+":"+mode)
		record = append(record, keptVolume{
			Destination: point.Destination,
			Volume:      point.Name,
			Form:        keptAsVolume,
			NoCopy:      false,
		})
	}

	for _, volume := range record[earlier:] {
		clog.Debug().
			Str("volume", volume.Volume).
			Str("destination", volume.Destination).
			Msg("Keeping anonymous volume for rebound container")
	}

	writeKeptVolumes(clog, config, record)
}

// restoreAnonymousVolumes mounts the volumes an earlier recreation kept by
// name anonymously again.
//
// Mounted by name, Docker never removes such a volume with the container, and
// Podman's removal of an anonymous volume would leave a new, empty named
// volume in its place. So the new container declares each one as the source
// container did, and gets a new anonymous volume there, as any recreated
// container does. A bind or mount that no longer names the recorded volume is
// left as it is. The record is removed from the new container's labels.
//
// Parameters:
//   - clog: Logger with container context fields.
//   - config: Config for the new container, whose labels are updated in
//     place.
//   - hostConfig: Host config for the new container, updated in place.
func restoreAnonymousVolumes(
	clog *zerolog.Logger,
	config *dockerContainer.Config,
	hostConfig *dockerContainer.HostConfig,
) {
	record := readKeptVolumes(clog, config)
	delete(config.Labels, labels.KeptVolumes)

	for _, volume := range record {
		destination := path.Clean(volume.Destination)
		names := func(source, target string) bool {
			return source == volume.Volume && path.Clean(target) == destination
		}

		switch volume.Form {
		case keptAsMount:
			for i, declaredMount := range hostConfig.Mounts {
				if declaredMount.Type == dockerMount.TypeVolume && names(declaredMount.Source, declaredMount.Target) {
					options := volumeOptions(declaredMount)
					options.NoCopy = volume.NoCopy
					hostConfig.Mounts[i].Source = ""
					hostConfig.Mounts[i].VolumeOptions = &options
				}
			}
		case keptAsBind, keptAsVolume:
			// A bind of the destination alone declares it again. A volume
			// the container declared neither way needs no bind.
			binds := hostConfig.Binds[:0]

			for _, bind := range hostConfig.Binds {
				parsed, ok := container.ParseBind(bind)

				switch {
				case !ok || !names(parsed.Source, parsed.Destination):
					binds = append(binds, bind)
				case volume.Form == keptAsBind:
					binds = append(binds, volume.Destination)
				default:
				}
			}

			hostConfig.Binds = binds
		default:
			continue
		}

		clog.Debug().
			Str("volume", volume.Volume).
			Str("destination", volume.Destination).
			Msg("Mounting kept volume anonymously again")
	}
}

// volumeOptions returns a copy of a mount's volume options, empty when it has
// none.
//
// Parameters:
//   - declaredMount: The mount whose options are copied.
//
// Returns:
//   - dockerMount.VolumeOptions: The copy, safe to change.
func volumeOptions(declaredMount dockerMount.Mount) dockerMount.VolumeOptions {
	if declaredMount.VolumeOptions == nil {
		return dockerMount.VolumeOptions{}
	}

	return *declaredMount.VolumeOptions
}

// readKeptVolumes returns the volumes a container's labels record as kept by
// name.
//
// A record that cannot be read counts as empty.
//
// Parameters:
//   - clog: Logger with container context fields.
//   - config: Config whose labels hold the record.
//
// Returns:
//   - []keptVolume: The recorded volumes, empty when there are none.
func readKeptVolumes(clog *zerolog.Logger, config *dockerContainer.Config) []keptVolume {
	value := config.Labels[labels.KeptVolumes]
	if value == "" {
		return nil
	}

	var record []keptVolume

	err := json.Unmarshal([]byte(value), &record)
	if err != nil {
		clog.Debug().Err(err).Msg("Ignoring unreadable record of kept volumes")

		return nil
	}

	return record
}

// writeKeptVolumes records the volumes a container keeps by name in its
// labels.
//
// Parameters:
//   - clog: Logger with container context fields.
//   - config: Config whose labels receive the record.
//   - record: The kept volumes. An empty record writes nothing.
func writeKeptVolumes(clog *zerolog.Logger, config *dockerContainer.Config, record []keptVolume) {
	if len(record) == 0 {
		return
	}

	value, err := json.Marshal(record)
	if err != nil {
		clog.Debug().Err(err).Msg("Failed to record kept volumes")

		return
	}

	if config.Labels == nil {
		config.Labels = make(map[string]string, 1)
	}

	config.Labels[labels.KeptVolumes] = string(value)
}

// syncComposeImageLabel points the Compose image label in config at the
// image the new container is created from.
//
// The label is copied from the source container, so after an update it still
// names the old image, and Docker Compose would recreate the container on its
// next deploy. The label is left unchanged when it is absent, when the new
// image has no value in the same form, or when an image inspect fails.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - clog: Logger with container context fields.
//   - sourceContainer: Source container the new container replaces.
//   - config: Creation config for the new container, updated in place.
//   - clientVersion: Docker API version used by the client.
func syncComposeImageLabel(
	ctx context.Context,
	api dockerClient.APIClient,
	clog *zerolog.Logger,
	sourceContainer *container.Container,
	config *dockerContainer.Config,
	clientVersion string,
) {
	label := config.Labels[composeLabels.ComposeImageLabel]

	info := sourceContainer.ContainerInfo()
	if label == "" || info == nil {
		return
	}

	value, ok, err := resolveComposeImageLabel(ctx, api, label, info.Image, config.Image, clientVersion)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to resolve Compose image label, keeping it unchanged")

		return
	}

	if !ok {
		clog.Debug().
			Str("label_value", label).
			Msg("Found no matching value for Compose image label, keeping it unchanged")

		return
	}

	if value == label {
		return
	}

	config.Labels[composeLabels.ComposeImageLabel] = value

	clog.Debug().
		Str("old_value", label).
		Str("new_value", value).
		Msg("Updated Compose image label")
}

// resolveComposeImageLabel inspects the old and new images and returns the
// Compose image label value for the new image.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - label: Compose image label value recorded for the old image.
//   - oldImageRef: ID of the image the source container runs.
//   - newImageRef: Reference the new container is created from.
//   - clientVersion: Docker API version used by the client.
//
// Returns:
//   - string: Label value for the new image.
//   - bool: False when the new image has no value in the same form as label.
//   - error: Non-nil if an image inspect fails.
func resolveComposeImageLabel(
	ctx context.Context,
	api dockerClient.APIClient,
	label, oldImageRef, newImageRef, clientVersion string,
) (string, bool, error) {
	oldImage, err := inspectImageWithManifests(ctx, api, oldImageRef, clientVersion)
	if err != nil {
		return "", false, err
	}

	newImage, err := inspectImageWithManifests(ctx, api, newImageRef, clientVersion)
	if err != nil {
		return "", false, err
	}

	value, ok := composeLabels.ImageLabelValue(label, oldImage, newImage)

	return value, ok, nil
}

// inspectImageWithManifests inspects an image on the local Docker daemon,
// including its manifests when clientVersion reports them.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - api: Docker API client.
//   - imageRef: Image ID or reference to inspect.
//   - clientVersion: Docker API version used by the client.
//
// Returns:
//   - *dockerImage.InspectResponse: Image metadata.
//   - error: Non-nil if the inspect fails.
func inspectImageWithManifests(
	ctx context.Context,
	api dockerClient.APIClient,
	imageRef, clientVersion string,
) (*dockerImage.InspectResponse, error) {
	var opts []dockerClient.ImageInspectOption
	if versions.GreaterThanOrEqualTo(clientVersion, manifestsMinAPIVersion) {
		opts = append(opts, dockerClient.ImageInspectWithManifests(true))
	}

	result, err := api.ImageInspect(ctx, imageRef, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", errInspectImageFailed, imageRef, err)
	}

	return &result.InspectResponse, nil
}

// attachNetworks connects a container to additional networks for legacy Docker API versions (< 1.44).
//
// It iterates through the provided network config, attaching all networks not included in the initial
// creation config, ensuring compatibility with Docker API < 1.44 where multiple network endpoints may fail.
//
// Parameters:
//   - ctx: Context for container API operations.
//   - api: Docker API client.
//   - containerID: ID of the container to attach networks to.
//   - networkConfig: Full network configuration with all desired endpoints.
//   - initialNetworkConfig: Network configuration used during container creation.
//   - clog: Logger with container-specific context for logging.
//
// Returns:
//   - error: Non-nil if attaching any network fails, nil on success.
func attachNetworks(
	ctx context.Context,
	api dockerClient.APIClient,
	containerID string,
	networkConfig *dockerNetwork.NetworkingConfig,
	initialNetworkConfig *dockerNetwork.NetworkingConfig,
	clog *zerolog.Logger,
) error {
	// Identify the initial network used during creation to skip it.
	var initialNetworkName string
	for name := range initialNetworkConfig.EndpointsConfig {
		initialNetworkName = name

		break
	}

	// Attach each additional network sequentially.
	for name, endpoint := range networkConfig.EndpointsConfig {
		if name != initialNetworkName && name != "" {
			clog.Debug().
				Str("network", name).
				Msg("Attaching additional network to container")

			_, err := api.NetworkConnect(
				ctx,
				name,
				dockerClient.NetworkConnectOptions{
					Container:      containerID,
					EndpointConfig: endpoint,
				},
			)
			if err != nil {
				clog.Error().
					Err(err).
					Str("network", name).
					Msg("Failed to attach additional network")

				return fmt.Errorf("failed to attach network %s: %w", name, err)
			}

			clog.Debug().
				Str("network", name).
				Msg("Successfully attached additional network")
		}
	}

	return nil
}

// handleCPUSettings applies CPU configuration based on the specified copy mode.
//
// It handles Podman compatibility by filtering NanoCPUs when necessary.
// Modes: "auto" (detect Podman and filter), "full" (copy all), "none" (strip all).
func handleCPUSettings(
	hostConfig *dockerContainer.HostConfig,
	cpuCopyMode string,
	isPodman bool,
	clog *zerolog.Logger,
) {
	switch cpuCopyMode {
	case "none":
		// Strip all CPU limits
		hostConfig.NanoCPUs = 0
		hostConfig.CPUShares = 0
		hostConfig.CPUQuota = 0
		hostConfig.CPUPeriod = 0
		hostConfig.CpusetCpus = ""
		hostConfig.CpusetMems = ""

		clog.Debug().Msg("Stripped all CPU settings")
	case "full":
		// Copy all CPU settings unchanged (default behavior)
		clog.Debug().Msg("Copied all CPU settings unchanged")
	case "auto":
		// Use isPodman flag to filter NanoCPUs if Podman
		if isPodman {
			hostConfig.NanoCPUs = 0

			clog.Debug().Msg("Detected Podman, filtered NanoCPUs for compatibility")
		} else {
			clog.Debug().Msg("Detected Docker, copied all CPU settings")
		}
	default:
		clog.Debug().
			Str("mode", cpuCopyMode).
			Msg("Unknown CPU copy mode, defaulting to full")
	}
}
