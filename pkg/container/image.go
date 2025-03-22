package container

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

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
	api client.APIClient
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
func newImageClient(api client.APIClient) imageClient {
	return imageClient{api: api}
}

// IsContainerStale determines if a container’s image is outdated compared to the latest available version.
// It checks the NoPull parameter to skip pulling if set, otherwise pulls the latest image and compares it
// with the container’s current image. If pulling is skipped or fails, it returns the current image ID.
//
// Parameters:
//   - target: The container whose image staleness is being checked.
//   - params: Update parameters, including whether pulling is disabled (NoPull).
//   - warnOnHeadFailed: Strategy for logging warnings on HEAD request failures.
//
// Returns:
//   - bool: True if the container’s image is stale (newer version exists), false otherwise.
//   - types.ImageID: The ID of the latest image (or current if not pulled).
//   - error: Any error encountered during pulling or inspection, nil if successful or skipped.
func (c imageClient) IsContainerStale(target types.Container, params types.UpdateParams, warnOnHeadFailed WarningStrategy) (bool, types.ImageID, error) {
	ctx := context.Background()

	if target.IsNoPull(params) {
		logrus.Debugf("Skipping image pull.")

		return false, target.SafeImageID(), nil // No pull, so assume not stale
	}

	if err := c.PullImage(ctx, target, warnOnHeadFailed); err != nil {
		return false, target.SafeImageID(), err // Return current ID on pull failure
	}

	return c.HasNewImage(ctx, target)
}

// HasNewImage checks if a newer image exists for the container’s image name.
// It inspects the latest image from the Docker daemon and compares its ID with the container’s current
// image ID. If they match, no update is needed; otherwise, a new image is detected.
//
// Parameters:
//   - ctx: Context for controlling the operation’s lifecycle.
//   - targetContainer: The container whose image is being checked.
//
// Returns:
//   - bool: True if a newer image is available, false if the current image is up-to-date.
//   - types.ImageID: The ID of the latest image.
//   - error: Any error from inspecting the image, nil if successful.
func (c imageClient) HasNewImage(ctx context.Context, targetContainer types.Container) (bool, types.ImageID, error) {
	currentImageID := types.ImageID(targetContainer.ContainerInfo().ContainerJSONBase.Image)
	imageName := targetContainer.ImageName()

	newImageInfo, err := c.api.ImageInspect(ctx, imageName)
	if err != nil {
		return false, currentImageID, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
	}

	newImageID := types.ImageID(newImageInfo.ID)
	if newImageID == currentImageID {
		logrus.Debugf("No new images found for %s", targetContainer.Name())

		return false, currentImageID, nil
	}

	logrus.Infof("Found new %s image (%s)", imageName, newImageID.ShortID())

	return true, newImageID, nil
}

// PullImage fetches the latest image for a container, skipping if the digest matches the current image.
// It first checks if the image is pinned (sha256), loads authentication, and uses a HEAD request to
// compare digests. If a pull is needed, it performs a full image pull and reads the response.
//
// Parameters:
//   - ctx: Context for controlling the operation’s lifecycle.
//   - targetContainer: The container whose image is being pulled.
//   - warnOnHeadFailed: Strategy for logging warnings on HEAD request failures.
//
// Returns:
//   - error: Any error from authentication, pulling, or reading the response, nil if successful or skipped.
func (c imageClient) PullImage(ctx context.Context, targetContainer types.Container, warnOnHeadFailed WarningStrategy) error {
	containerName := targetContainer.Name()
	imageName := targetContainer.ImageName()
	fields := logrus.Fields{"image": imageName, "container": containerName}

	// Prevent pulling pinned images (e.g., sha256: references), as they are immutable.
	if strings.HasPrefix(imageName, "sha256:") {
		return errPinnedImage
	}

	logrus.WithFields(fields).Debugf("Trying to load authentication credentials.")

	opts, err := registry.GetPullOptions(imageName)
	if err != nil {
		logrus.Debugf("Error loading authentication credentials %s", err)

		return fmt.Errorf("failed to get pull options for %s: %w", imageName, err)
	}

	// Log if authentication credentials are successfully loaded.
	if opts.RegistryAuth != "" {
		logrus.Debug("Credentials loaded")
	}

	// Skip the pull if the digest matches the current image.
	if c.shouldSkipPull(ctx, targetContainer, opts.RegistryAuth, warnOnHeadFailed, fields) {
		return nil
	}

	// Perform the full image pull if needed.
	return c.performImagePull(ctx, imageName, opts, fields)
}

// shouldSkipPull determines if an image pull can be skipped based on digest comparison.
// It performs a HEAD request to compare the current image digest with the remote digest. If the digests
// match, pulling is unnecessary; if the HEAD request fails, it logs the error and opts for a full pull.
//
// Parameters:
//   - ctx: Context for controlling the operation’s lifecycle.
//   - targetContainer: The container whose image digest is being checked.
//   - auth: Registry authentication credentials for the HEAD request.
//   - warnOnHeadFailed: Strategy for logging warnings on HEAD request failures.
//   - fields: Logrus fields for consistent logging context.
//
// Returns:
//   - bool: True if the pull should be skipped (digests match), false if it should proceed.
func (c imageClient) shouldSkipPull(ctx context.Context, targetContainer types.Container, auth string, warnOnHeadFailed WarningStrategy, fields logrus.Fields) bool {
	logrus.WithFields(fields).Debugf("Checking if pull is needed")

	warn := c.warnOnHeadFailed(targetContainer, warnOnHeadFailed)
	match, err := digest.CompareDigest(ctx, targetContainer, auth)
	logrus.WithFields(fields).Debugf("Digest match: %v, error: %v", match, err)

	switch {
	case err != nil:
		// HEAD request failed; log based on warning strategy and proceed with pull.
		headLevel := logrus.DebugLevel
		if warn {
			headLevel = logrus.WarnLevel
		}

		logrus.WithFields(fields).Logf(headLevel, "Could not do a head request for %q, falling back to regular pull.", targetContainer.ImageName())
		logrus.WithFields(fields).Log(headLevel, "Reason: ", err)

		return false
	case match:
		// Digests match; no pull needed.
		logrus.Debug("No pull needed. Skipping image.")

		return true
	default:
		// Digests differ; proceed with pull.
		logrus.Debug("Digests did not match, doing a pull.")

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
func (c imageClient) performImagePull(ctx context.Context, imageName string, opts image.PullOptions, fields logrus.Fields) error {
	logrus.WithFields(fields).Debugf("Pulling image")

	response, err := c.api.ImagePull(ctx, imageName, opts)
	if err != nil {
		logrus.Debugf("Error pulling image %s, %s", imageName, err)

		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}

	defer response.Close()

	// Read the entire response to ensure the pull completes successfully.
	if _, err = io.ReadAll(response); err != nil {
		logrus.Error(err)

		return fmt.Errorf("failed to read pull response for %s: %w", imageName, err)
	}

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
	logrus.Infof("Removing image %s", imageID.ShortID())

	items, err := c.api.ImageRemove(
		context.Background(),
		string(imageID),
		image.RemoveOptions{
			Force:         true,
			PruneChildren: true,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to remove image %s: %w", imageID, err)
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

		fields := logrus.Fields{"deleted": deleted.String(), "untagged": untagged.String()}
		logrus.WithFields(fields).Debug("Image removal completed")
	}

	return nil
}

// warnOnHeadFailed decides whether to warn about failed HEAD requests during image pulls.
// It evaluates the warning strategy: "always" returns true, "never" returns false, and "auto" delegates
// to registry-specific logic.
//
// Parameters:
//   - targetContainer: The container whose image is being checked.
//   - warnOnHeadFailed: The configured warning strategy.
//
// Returns:
//   - bool: True if a warning should be logged, false otherwise.
func (c imageClient) warnOnHeadFailed(targetContainer types.Container, warnOnHeadFailed WarningStrategy) bool {
	if warnOnHeadFailed == WarnAlways {
		return true
	}

	if warnOnHeadFailed == WarnNever {
		return false
	}

	return registry.WarnOnAPIConsumption(targetContainer)
}
