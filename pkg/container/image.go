package container

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sirupsen/logrus"

	cerrdefs "github.com/containerd/errdefs"
	dockerImage "github.com/docker/docker/api/types/image"
	dockerClient "github.com/docker/docker/client"

	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/nicholas-fedor/watchtower/pkg/registry/digest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Warning strategies for HEAD request failures.
const (
	// WarnAlways indicates warnings should always be logged for HEAD request failures.
	WarnAlways WarningStrategy = "always"
	// WarnNever indicates warnings should never be logged for HEAD request failures.
	WarnNever WarningStrategy = "never"
	// WarnAuto indicates warnings should be logged for HEAD request failures based on registry heuristics.
	WarnAuto WarningStrategy = "auto"
)

// WarningStrategy defines the policy for logging warnings when HEAD requests fail during image pulls.
//
// It allows configuration of verbosity:
//   - "always" logs all failures
//   - "never" suppresses them
//   - "auto" delegates to registry-specific logic (e.g., WarnOnAPIConsumption).
type WarningStrategy string

// imageClient manages image-related operations for Watchtower.
//
// It uses a Docker API client for image tasks.
type imageClient struct {
	api dockerClient.APIClient
}

// IsContainerStale determines if a container’s image is outdated.
//
// It skips pulling if NoPull is set, otherwise pulls and compares images.
//
// Parameters:
//   - ctx: Context for operation control.
//   - sourceContainer: Container to check.
//   - params: Update parameters (e.g., NoPull flag).
//   - warnOnHeadFailed: Strategy for logging warnings on HEAD request failures.
//
// Returns:
//   - bool: True if image is stale, false otherwise.
//   - types.ImageID: Latest image ID (or current if not pulled).
//   - error: Non-nil if pull or inspection fails, nil on success or skipped.
func (c imageClient) IsContainerStale(
	ctx context.Context,
	sourceContainer types.Container,
	params types.UpdateParams,
	warnOnHeadFailed WarningStrategy,
) (bool, types.ImageID, error) {
	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	})

	// Skip pull if NoPull is enabled.
	if sourceContainer.IsNoPull(params) {
		return c.checkLocalImageStaleness(ctx, sourceContainer, clog)
	}

	err := c.PullImage(ctx, sourceContainer, warnOnHeadFailed)
	if err != nil {
		clog.WithError(err).Debug("Failed to pull image")

		return false, sourceContainer.ImageID(), err
	}

	// Check for a newer image.
	return c.HasNewImage(ctx, sourceContainer)
}

// HasNewImage checks if a newer image exists for the container.
//
// It compares the latest image ID with the current one.
//
// Parameters:
//   - ctx: Context for operation control.
//   - sourceContainer: Container to check.
//
// Returns:
//   - bool: True if a newer image exists, false if current is latest.
//   - types.ImageID: Latest image ID.
//   - error: Non-nil if inspection fails, nil on success.
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

	// Inspect the latest image by name.
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

	// Compare IDs to determine staleness.
	newImageID := types.ImageID(newImageInfo.ID)
	if newImageID == currentImageID {
		clog.Debug("No new image found")

		return false, currentImageID, nil
	}

	// Log full image name and ID
	clog.WithField("new_id", newImageID.ShortID()).Info("Found new image")

	return true, newImageID, nil
}

// PullImage fetches the latest image for a container.
//
// It skips pinned images and checks digests before pulling.
//
// Parameters:
//   - ctx: Context for operation control.
//   - sourceContainer: Container whose image to pull.
//
// Returns:
//   - error: Non-nil if pull fails, nil on success or skip.
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

	// Skip pulling immutable sha256-pinned images.
	if strings.HasPrefix(sourceContainer.ImageName(), "sha256:") {
		clog.Debug("Skipping pull of pinned sha256 image")

		return errPinnedImage
	}

	clog.Debug("Loading authentication credentials")

	// Get pull options with authentication.
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

	// Perform full image pull.
	return c.performImagePull(ctx, sourceContainer.ImageName(), opts, fields)
}

// RemoveImageByID deletes an image from the Docker host.
//
// It removes the image with force and pruning, logging details if debug enabled.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - imageID: ID of the image to remove.
//   - imageName: Name of the image to remove (for logging purposes).
//
// Returns:
//   - error: Non-nil if removal fails, nil on success.
func (c imageClient) RemoveImageByID(ctx context.Context, imageID types.ImageID, imageName string) error {
	clog := logrus.WithFields(logrus.Fields{
		"image_id":   imageID.ShortID(),
		"image_name": imageName,
	})
	clog.WithField("notify", "yes").Info("Removing image")

	// Perform image removal with force and pruning.
	items, err := c.api.ImageRemove(
		ctx,
		string(imageID),
		dockerImage.RemoveOptions{
			Force:         true,
			PruneChildren: true,
		},
	)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			clog.WithError(err).Debug("Image not found, no removal needed")

			return fmt.Errorf("%w: %s", err, imageID)
		}

		clog.WithError(err).Debug("Failed to remove image")

		return fmt.Errorf("%w: %s: %w", errRemoveImageFailed, imageID, err)
	}

	// Log removal details if debug is enabled.
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

		logrus.WithFields(logrus.Fields{
			"deleted":    deleted.String(),
			"image_id":   imageID.ShortID(),
			"image_name": imageName,
			"untagged":   untagged.String(),
		}).Debug("Image removal details")
	}

	clog.Debug("Cleaned up old image")

	return nil
}

// newImageClient creates a new imageClient instance.
//
// Parameters:
//   - api: Docker API client.
//
// Returns:
//   - imageClient: Initialized client for image operations.
func newImageClient(api dockerClient.APIClient) imageClient {
	return imageClient{api: api}
}

// shouldSkipPull determines if an image pull can be skipped.
//
// It compares digests via HEAD request to avoid unnecessary pulls.
//
// Parameters:
//   - ctx: Context for operation control.
//   - sourceContainer: Container to check.
//   - auth: Registry authentication credentials.
//   - warnOnHeadFailed: Strategy for logging warnings on HEAD request failures.
//   - fields: Logging fields for context.
//
// Returns:
//   - bool: True if pull can be skipped, false otherwise.
func (c imageClient) shouldSkipPull(
	ctx context.Context,
	sourceContainer types.Container,
	registryAuth string,
	warnOnHeadFailed WarningStrategy,
	fields logrus.Fields,
) bool {
	clog := logrus.WithFields(fields)
	clog.Debug("Checking if pull is needed")

	warn := c.warnOnHeadFailed(sourceContainer, warnOnHeadFailed)
	// Compare current and remote digests.
	match, err := digest.CompareDigest(ctx, c.api, sourceContainer, registryAuth)
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
		// Digest retrieval failed; log based on warning strategy and proceed with pull.
		headLevel := logrus.DebugLevel
		if warn {
			headLevel = logrus.WarnLevel
		}

		clog.WithError(err).
			Log(headLevel, "Digest retrieval failed, falling back to full pull")

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

// performImagePull executes a full image pull.
//
// It pulls the image and reads the response to ensure completion.
//
// Parameters:
//   - ctx: Context for operation control.
//   - imageName: Image to pull.
//   - opts: Pull options with auth.
//   - fields: Logging fields for context.
//
// Returns:
//   - error: Non-nil if pull or read fails, nil on success.
func (c imageClient) performImagePull(
	ctx context.Context,
	imageName string,
	opts dockerImage.PullOptions,
	fields logrus.Fields,
) error {
	clog := logrus.WithFields(fields)
	clog.Debug("Initiating image pull")

	// Start the image pull.
	response, err := c.api.ImagePull(ctx, imageName, opts)
	if err != nil {
		clog.WithError(err).Debug("Failed to initiate image pull")

		return fmt.Errorf("%w: %s: %w", errPullImageFailed, imageName, err)
	}
	defer response.Close()

	// Read response to complete the pull.
	_, err = io.ReadAll(response)
	if err != nil {
		clog.WithError(err).Debug("Failed to read image pull response")

		return fmt.Errorf("%w: %s: %w", errReadPullResponseFailed, imageName, err)
	}

	clog.Debug("Image pull completed")

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

// checkLocalImageStaleness checks if a container's image is stale without pulling.
//
// It performs local image inspection and comparison, handling logging.
//
// Parameters:
//   - ctx: Context for operation control.
//   - sourceContainer: Container to check.
//   - clog: Logger with container and image fields.
//
// Returns:
//   - bool: True if image is stale, false otherwise.
//   - types.ImageID: Latest image ID.
//   - error: Non-nil if inspection fails, nil on success.
func (c imageClient) checkLocalImageStaleness(
	ctx context.Context,
	sourceContainer types.Container,
	clog *logrus.Entry,
) (bool, types.ImageID, error) {
	clog.Debug("Skipping image pull due to no-pull setting - checking local image only")
	clog.WithField("current_image_id", sourceContainer.ImageID()).
		Debug("Current container image ID")

	stale, latestID, err := c.HasNewImage(ctx, sourceContainer)
	if err != nil {
		clog.WithError(err).Debug("Failed to check local image")

		return false, sourceContainer.ImageID(), err
	}

	clog.WithFields(logrus.Fields{
		"stale":           stale,
		"latest_image_id": latestID,
	}).Debug("Local image check result")

	return stale, latestID, nil
}
