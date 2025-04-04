package container

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sirupsen/logrus"

	dockerImageType "github.com/docker/docker/api/types/image"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/nicholas-fedor/watchtower/pkg/registry/digest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	// WarnAlways indicates warnings should always be logged for HEAD request failures.
	WarnAlways WarningStrategy = "always"
	// WarnNever indicates warnings should never be logged for HEAD request failures.
	WarnNever WarningStrategy = "never"
	// WarnAuto indicates warnings should be logged for HEAD request failures based on registry heuristics.
	WarnAuto WarningStrategy = "auto"
)

// imageClient manages image-related operations for Watchtower.
// It encapsulates a Docker API client to perform tasks like pulling and inspecting images.
type imageClient struct {
	api dockerClient.APIClient
}

// WarningStrategy defines the policy for logging warnings when HEAD requests fail during image pulls.
// It allows configuration of verbosity: "always" logs all failures, "never" suppresses them, and "auto"
// delegates to registry-specific logic (e.g., WarnOnAPIConsumption).
type WarningStrategy string

// newImageClient creates a new imageClient instance with the given Docker API client.
// It initializes the client for subsequent image operations.
//
// Parameters:
//   - api: The Docker API client used to interact with the Docker daemon.
//
// Returns:
//
//	An initialized imageClient ready to manage image-related tasks.
func newImageClient(api dockerClient.APIClient) imageClient {
	return imageClient{api: api}
}

// IsContainerStale determines if a container’s image is outdated compared to the latest available version.
// It checks the NoPull parameter to skip pulling if set, otherwise pulls the latest image and compares it
// with the container’s current image. If pulling is skipped or fails, it returns the current image ID.
//
// Parameters:
//   - sourceContainer: The container whose image staleness is being checked.
//   - params: Update parameters, including whether pulling is disabled (NoPull).
//   - warnOnHeadFailed: Strategy for logging warnings on HEAD request failures.
//
// Returns:
//   - bool: True if the container’s image is stale (newer version exists), false otherwise.
//   - types.ImageID: The ID of the latest image (or current if not pulled).
//   - error: Any error encountered during pulling or inspection, nil if successful or skipped.
func (c imageClient) IsContainerStale(
	sourceContainer types.Container,
	params types.UpdateParams,
	warnOnHeadFailed WarningStrategy,
) (bool, types.ImageID, error) {
	ctx := context.Background()
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	})

	if sourceContainer.IsNoPull(params) {
		clog.Debug("Skipping image pull due to no-pull setting")

		return false, sourceContainer.SafeImageID(), nil
	}

	if err := c.PullImage(ctx, sourceContainer, warnOnHeadFailed); err != nil {
		clog.WithError(err).Debug("Failed to pull image")

		return false, sourceContainer.SafeImageID(), err
	}

	return c.HasNewImage(ctx, sourceContainer)
}

// HasNewImage checks if a newer image exists for the container’s image name.
// It inspects the latest image from the Docker daemon and compares its ID with the container’s current
// image ID. If they match, no update is needed; otherwise, a new image is detected.
//
// Parameters:
//   - ctx: Context for controlling the operation’s lifecycle.
//   - sourceContainer: The container whose image is being checked.
//
// Returns:
//   - bool: True if a newer image is available, false if the current image is up-to-date.
//   - types.ImageID: The ID of the latest image.
//   - error: Any error from inspecting the image, nil if successful.
func (c imageClient) HasNewImage(
	ctx context.Context,
	sourceContainer types.Container,
) (bool, types.ImageID, error) {
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	})
	currentImageID := types.ImageID(sourceContainer.ContainerInfo().Image)

	clog.Debug("Inspecting latest image")

	newImageInfo, err := c.api.ImageInspect(ctx, sourceContainer.ImageName())
	if err != nil {
		clog.WithError(err).Debug("Failed to inspect latest image")

		return false, currentImageID, fmt.Errorf(
			"%w: %s: %w",
			errInspectImageFailed,
			sourceContainer.ImageName(),
			err,
		)
	}

	newImageID := types.ImageID(newImageInfo.ID)
	if newImageID == currentImageID {
		clog.Debug("No new image found")

		return false, currentImageID, nil
	}

	clog.WithField("new_id", newImageID.ShortID()).Info("Found new image")

	return true, newImageID, nil
}

// PullImage fetches the latest image for a container, skipping if the digest matches the current image.
// It first checks if the image is pinned (sha256), loads authentication, and uses a HEAD request to
// compare digests. If a pull is needed, it performs a full image pull and reads the response.
//
// Parameters:
//   - ctx: Context for controlling the operation’s lifecycle.
//   - sourceContainer: The container whose image is being pulled.
//   - warnOnHeadFailed: Strategy for logging warnings on HEAD request failures.
//
// Returns:
//   - error: Any error from authentication, pulling, or reading the response, nil if successful or skipped.
func (c imageClient) PullImage(
	ctx context.Context,
	sourceContainer types.Container,
	warnOnHeadFailed WarningStrategy,
) error {
	fields := logrus.Fields{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	}
	clog := logrus.WithFields(fields)

	// Prevent pulling pinned images (e.g., sha256: references), as they are immutable.
	if strings.HasPrefix(sourceContainer.ImageName(), "sha256:") {
		clog.Warn("Skipping pull of pinned sha256 image")

		return errPinnedImage
	}

	clog.Debug("Loading authentication credentials")

	opts, err := registry.GetPullOptions(sourceContainer.ImageName())
	if err != nil {
		clog.WithError(err).Debug("Failed to load authentication credentials")

		return fmt.Errorf("%w: %s: %w", errPullImageFailed, sourceContainer.ImageName(), err)
	}

	// Log if authentication credentials are successfully loaded.
	if opts.RegistryAuth != "" {
		clog.Debug("Authentication credentials loaded")
	}

	// Skip the pull if the digest matches the current image.
	if c.shouldSkipPull(ctx, sourceContainer, opts.RegistryAuth, warnOnHeadFailed, fields) {
		return nil
	}

	// Perform the full image pull if needed.
	return c.performImagePull(ctx, sourceContainer.ImageName(), opts, fields)
}

// shouldSkipPull determines if an image pull can be skipped based on digest comparison.
// It performs a HEAD request to compare the current image digest with the remote digest. If the digests
// match, pulling is unnecessary; if the HEAD request fails, it logs the error and opts for a full pull.
//
// Parameters:
//   - ctx: Context for controlling the operation’s lifecycle.
//   - sourceContainer: The container whose image digest is being checked.
//   - auth: Registry authentication credentials for the HEAD request.
//   - warnOnHeadFailed: Strategy for logging warnings on HEAD request failures.
//   - fields: Logrus fields for consistent logging context.
//
// Returns:
//   - bool: True if the pull should be skipped (digests match), false if it should proceed.
func (c imageClient) shouldSkipPull(
	ctx context.Context,
	sourceContainer types.Container,
	auth string,
	warnOnHeadFailed WarningStrategy,
	fields logrus.Fields,
) bool {
	clog := logrus.WithFields(fields)
	clog.Debug("Checking if pull is needed")

	warn := c.warnOnHeadFailed(sourceContainer, warnOnHeadFailed)

	match, err := digest.CompareDigest(ctx, sourceContainer, auth)
	if err != nil {
		clog.WithFields(logrus.Fields{
			"match": match,
			"error": err,
		}).Debug("Digest comparison result")
	} else {
		clog.WithField("match", match).Debug("Digest comparison result")
	}

	switch {
	case err != nil:
		// HEAD request failed; log based on warning strategy and proceed with pull.
		headLevel := logrus.DebugLevel
		if warn {
			headLevel = logrus.WarnLevel
		}

		clog.WithError(err).
			Log(headLevel, "HEAD request failed, falling back to full pull")

		return false
	case match:
		// Digests match; no pull needed.
		clog.Debug("Digest match, skipping pull")

		return true
	default:
		// Digests differ; proceed with pull.
		clog.Debug("Digest mismatch, proceeding with pull")

		return false
	}
}

// performImagePull executes the full image pull operation and reads the response.
// It initiates the pull with the provided options and ensures the response is fully consumed to complete
// the operation.
//
// Parameters:
//   - ctx: Context for controlling the operation’s lifecycle.
//   - imageName: The name of the image to pull.
//   - opts: Pull options, including authentication credentials.
//   - fields: Logrus fields for consistent logging context.
//
// Returns:
//   - error: Any error from pulling or reading the response, nil if successful.
func (c imageClient) performImagePull(
	ctx context.Context,
	imageName string,
	opts dockerImageType.PullOptions,
	fields logrus.Fields,
) error {
	clog := logrus.WithFields(fields)
	clog.Debug("Initiating image pull")

	response, err := c.api.ImagePull(ctx, imageName, opts)
	if err != nil {
		clog.WithError(err).Debug("Failed to initiate image pull")

		return fmt.Errorf("%w: %s: %w", errPullImageFailed, imageName, err)
	}
	defer response.Close()

	// Read the entire response to ensure the pull completes successfully.
	if _, err = io.ReadAll(response); err != nil {
		clog.WithError(err).Debug("Failed to read image pull response")

		return fmt.Errorf("%w: %s: %w", errReadPullResponseFailed, imageName, err)
	}

	clog.Debug("Image pull completed")

	return nil
}

// RemoveImageByID deletes an image from the Docker host by its ID.
// It removes the image with force and pruning options, logging detailed removal info if debug logging
// is enabled.
//
// Parameters:
//   - imageID: The ID of the image to remove.
//
// Returns:
//   - error: Any error from the removal operation, nil if successful.
func (c imageClient) RemoveImageByID(imageID types.ImageID) error {
	clog := logrus.WithField("image_id", imageID.ShortID())
	clog.Info("Removing image")

	items, err := c.api.ImageRemove(
		context.Background(),
		string(imageID),
		dockerImageType.RemoveOptions{
			Force:         true,
			PruneChildren: true,
		},
	)
	if err != nil {
		if dockerClient.IsErrNotFound(err) {
			clog.WithError(err).Debug("Image not found, no removal needed")

			return fmt.Errorf("%w: %s", err, imageID)
		}

		clog.WithError(err).Debug("Failed to remove image")

		return fmt.Errorf("%w: %s: %w", errRemoveImageFailed, imageID, err)
	}

	// Log detailed removal info if debug level is enabled.
	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		deleted := strings.Builder{}
		untagged := strings.Builder{}

		for _, item := range items {
			if item.Deleted != "" {
				if deleted.Len() > 0 {
					deleted.WriteString(", ")
				}

				deleted.WriteString(types.ImageID(item.Deleted).ShortID())
			}

			if item.Untagged != "" {
				if untagged.Len() > 0 {
					untagged.WriteString(", ")
				}

				untagged.WriteString(types.ImageID(item.Untagged).ShortID())
			}
		}

		clog.WithFields(logrus.Fields{
			"deleted":  deleted.String(),
			"untagged": untagged.String(),
		}).Debug("Image removal details")
	}

	return nil
}

// warnOnHeadFailed decides whether to warn about failed HEAD requests during image pulls.
// It evaluates the warning strategy: "always" returns true, "never" returns false, and "auto" delegates
// to registry-specific logic.
//
// Parameters:
//   - sourceContainer: The container whose image is being checked.
//   - warnOnHeadFailed: The configured warning strategy.
//
// Returns:
//   - bool: True if a warning should be logged, false otherwise.
func (c imageClient) warnOnHeadFailed(
	sourceContainer types.Container,
	warnOnHeadFailed WarningStrategy,
) bool {
	if warnOnHeadFailed == WarnAlways {
		return true
	}

	if warnOnHeadFailed == WarnNever {
		return false
	}

	return registry.WarnOnAPIConsumption(sourceContainer)
}
