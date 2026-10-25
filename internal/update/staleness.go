package update

import (
	"errors"
	"fmt"
	"strings"

	"github.com/distribution/reference"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Errors for checking images for updates.
var (
	// errInvalidImageReference indicates an invalid image reference that cannot be processed.
	errInvalidImageReference = errors.New("invalid image reference")
)

// shouldUpdateContainer determines if a container should be updated
// based on its staleness and update parameters.
//
// It checks multiple conditions:
//   - The container must be stale (outdated image)
//   - The container must not be monitor-only
//   - Watchtower containers are skipped in run-once mode
//   - Watchtower self-updates are skipped if SkipSelfUpdate is true
//
// Parameters:
//   - container: The container to check.
//   - stale: Whether the container's image is outdated.
//   - config: Update parameters controlling update behavior.
//
// Returns:
//   - bool: True if the container should be updated, false otherwise.
func shouldUpdateContainer(
	container types.Container,
	stale bool,
	config types.UpdateParams,
) bool {
	// Must be stale to update
	if !stale {
		return false
	}

	// Skip old Watchtower containers — they are predecessors from a
	// self-update and should only be removed, never updated.
	if filters.IsOldWatchtower(container) {
		return false
	}

	// Skip monitor-only containers
	if container.IsMonitorOnly(config) {
		return false
	}

	// Skip Watchtower self-update in run-once mode
	if config.RunOnce && container.IsWatchtower() {
		return false
	}

	// Skip Watchtower self-update if SkipSelfUpdate is true
	if config.SkipSelfUpdate && container.IsWatchtower() {
		return false
	}

	// Skip other Watchtower containers from self-updates
	if container.IsWatchtower() && config.CurrentContainerID != "" &&
		container.ID() != config.CurrentContainerID {
		return false
	}

	return true
}

// parseReference validates a Docker image reference with logging.
//
// Parameters:
//   - imageName: Image name to parse.
//   - configImage: Config.Image value for log context.
//   - fallbackImage: Fallback image name for log context.
//   - cont: Container being processed (for log fields).
//
// Returns:
//   - error: Non-nil if the image name cannot be parsed as a Docker reference.
func parseReference(log *zerolog.Logger, imageName, configImage, fallbackImage string,
	cont types.Container,
) error {
	// Set up logging with container and image details.
	clogVal := log.With().
		Str("container", cont.Name()).
		Str("image", imageName).
		Logger()
	clog := &clogVal

	// Parse the image reference using the Docker reference library.
	normalizedRef, err := reference.ParseDockerRef(imageName)
	if err != nil {
		clog.Debug().
			Err(err).
			Str("image_name", imageName).
			Msg("Failed to parse image reference")

		return fmt.Errorf(
			"failed to parse image reference %s: %w",
			imageName,
			err,
		)
	}

	// Log successful parsing with reference type and context.
	clog.Debug().
		Str("image_name", imageName).
		Str("config_image", configImage).
		Str("fallback_image", fallbackImage).
		Str("ref_type", fmt.Sprintf("%T", normalizedRef)).
		Msg("Parsed image reference")

	return nil
}

// isPinned checks if a container's image is pinned by a digest reference.
//
// It resolves a usable image name from ImageName(), Config.Image, or a fallback,
// then delegates pin detection to container.IsImagePinnedByDigest. Pin detection
// runs before parse-fallback so a digest reference is not replaced by a non-pinned
// fallback when ParseDockerRef fails. If pinned, it marks the container as scanned.
//
// Parameters:
//   - cont: The container to check for a pinned image.
//   - progress: The progress tracker to update for scanned or skipped containers.
//   - params: Update parameters for monitor-only check.
//
// Returns:
//   - bool: True if the image is pinned by digest, false otherwise.
//   - error: Non-nil if no valid image reference can be resolved, nil on success.
func isPinned(
	log *zerolog.Logger,
	cont types.Container,
	progress *state.Progress,
	config types.UpdateParams,
	gitSession *gitSession,
) (bool, error) {
	// Set up logging with container and image details for debugging.
	clogVal := log.With().
		Str("container", cont.Name()).
		Str("image", cont.ImageName()).
		Logger()
	clog := &clogVal

	// Get initial image name and configuration.
	imageName := cont.ImageName()

	var configImage string
	if info := cont.ContainerInfo(); info != nil && info.Config != nil {
		configImage = info.Config.Image
	}

	fallbackImage := getFallbackImage(cont)

	// Check if ImageName is invalid and fall back to Config.Image or a derived name.
	if isInvalidImageName(imageName) {
		clog.Debug().
			Str("invalid_image", imageName).
			Msg("Invalid ImageName detected")

		if configImage != "" && !isInvalidImageName(configImage) {
			imageName = configImage
			clog.Debug().
				Str("config_image", configImage).
				Msg("Using Config.Image as fallback")
		} else {
			imageName = fallbackImage
			clog.Debug().
				Str("fallback_image", fallbackImage).
				Msg("Using derived fallback image")
		}
	}

	// If the final imageName is still invalid, skip the container.
	if isInvalidImageName(imageName) {
		return false, errInvalidImageReference
	}

	// Detect digests before parse-fallback.
	// A repo@sha256 reference must stay pinned even when ParseDockerRef fails
	// for unrelated reasons.
	if containerImage.IsImagePinnedByDigest(imageName) {
		if gitSession.watching(log, cont, config) {
			clog.Debug().Msg("Pinned digest uses Git path because watcher is on")

			return false, nil
		}

		clog.Debug().
			Bool("is_digested", true).
			Msg("Pinned image detected, marking as scanned")
		progress.AddScanned(log,
			cont,
			cont.ImageID(),
			config,
		)

		return true, nil
	}

	// Non-pinned names must still be parseable. Retry with fallback when needed.
	err := parseReference(log, imageName, configImage, fallbackImage, cont)
	if err != nil {
		if imageName != fallbackImage {
			clog.Debug().Msg("Retrying with fallback image")

			fallbackErr := parseReference(log,
				fallbackImage,
				configImage,
				fallbackImage,
				cont,
			)
			if fallbackErr != nil {
				return false, err
			}

			return false, nil
		}

		return false, err
	}

	return false, nil
}

// getFallbackImage derives a fallback image name from container info.
// Uses the container name with ":latest" as the fallback image.
func getFallbackImage(container types.Container) string {
	return container.Name() + ":latest"
}

// isInvalidImageName checks if an image name is invalid.
// Returns true if the name is empty, ":latest", or starts with ":".
func isInvalidImageName(name string) bool {
	return name == "" || name == ":latest" || strings.HasPrefix(name, ":")
}
