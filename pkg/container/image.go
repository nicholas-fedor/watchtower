package container

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/nicholas-fedor/watchtower/pkg/registry/digest"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/sirupsen/logrus"
)

// imageClient manages image-related operations for Watchtower.
// It wraps the Docker API client for image-specific tasks.
type imageClient struct {
	api client.APIClient
}

// newImageClient creates a new imageClient instance.
// It initializes the client with the provided Docker API client.
func newImageClient(api client.APIClient) imageClient {
	return imageClient{api: api}
}

// IsContainerStale determines if a container’s image is outdated compared to the latest available version.
// It pulls the latest image if needed and compares it with the current image.
// Returns whether the container is stale, the latest image ID, and any error encountered.
func (c imageClient) IsContainerStale(target types.Container, params types.UpdateParams, warnOnHeadFailed WarningStrategy) (bool, types.ImageID, error) {
	ctx := context.Background()

	if target.IsNoPull(params) {
		logrus.Debugf("Skipping image pull.")

		return false, target.SafeImageID(), nil // Skip HasNewImage when NoPull is true
	}

	if err := c.PullImage(ctx, target, warnOnHeadFailed); err != nil {
		return false, target.SafeImageID(), err
	}

	return c.HasNewImage(ctx, target)
}

// HasNewImage checks if a newer image exists for the container’s image name.
// It compares the current image ID with the latest available ID from the Docker daemon.
// Returns whether a new image exists, the latest image ID, and any error encountered.
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

// PullImage fetches the latest image for a container, optionally skipping if the digest matches.
// It performs a HEAD request to compare digests and falls back to a full pull if needed.
// Returns an error if the pull fails or if the image is pinned (sha256).
func (c imageClient) PullImage(ctx context.Context, targetContainer types.Container, warnOnHeadFailed WarningStrategy) error {
	containerName := targetContainer.Name()
	imageName := targetContainer.ImageName()

	fields := logrus.Fields{
		"image":     imageName,
		"container": containerName,
	}

	// Prevent pulling pinned images.
	if strings.HasPrefix(imageName, "sha256:") {
		return errPinnedImage
	}

	logrus.WithFields(fields).Debugf("Trying to load authentication credentials.")

	opts, err := registry.GetPullOptions(imageName)
	if err != nil {
		logrus.Debugf("Error loading authentication credentials %s", err)

		return fmt.Errorf("failed to get pull options for %s: %w", imageName, err)
	}

	if opts.RegistryAuth != "" {
		logrus.Debug("Credentials loaded")
	}

	logrus.WithFields(fields).Debugf("Checking if pull is needed")

	warn := c.warnOnHeadFailed(targetContainer, warnOnHeadFailed)
	match, err := digest.CompareDigest(targetContainer, opts.RegistryAuth)
	logrus.WithFields(fields).Debugf("Digest match: %v, error: %v", match, err)

	switch {
	case err != nil:
		headLevel := logrus.DebugLevel
		if warn {
			headLevel = logrus.WarnLevel
		}

		logrus.WithFields(fields).Logf(headLevel, "Could not do a head request for %q, falling back to regular pull.", imageName)
		logrus.WithFields(fields).Log(headLevel, "Reason: ", err)
	case match:
		logrus.Debug("No pull needed. Skipping image.")

		return nil
	default:
		logrus.Debug("Digests did not match, doing a pull.")
	}

	logrus.WithFields(fields).Debugf("Pulling image")

	response, err := c.api.ImagePull(ctx, imageName, opts)
	if err != nil {
		logrus.Debugf("Error pulling image %s, %s", imageName, err)

		return fmt.Errorf("failed to pull image %s: %w", imageName, err)
	}

	defer response.Close()
	// Read the response fully to avoid aborting the pull prematurely.
	if _, err = io.ReadAll(response); err != nil {
		logrus.Error(err)

		return fmt.Errorf("failed to read pull response for %s: %w", imageName, err)
	}

	return nil
}

// RemoveImageByID deletes an image from the Docker host by its ID.
// It logs detailed removal info if debug logging is enabled.
// Returns an error if the removal fails.
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

	if logrus.IsLevelEnabled(logrus.DebugLevel) {
		deleted := strings.Builder{}
		untagged := strings.Builder{}

		for _, item := range items {
			if item.Deleted != "" {
				if deleted.Len() > 0 {
					deleted.WriteString(`, `)
				}

				deleted.WriteString(types.ImageID(item.Deleted).ShortID())
			}

			if item.Untagged != "" {
				if untagged.Len() > 0 {
					untagged.WriteString(`, `)
				}

				untagged.WriteString(types.ImageID(item.Untagged).ShortID())
			}
		}

		fields := logrus.Fields{`deleted`: deleted.String(), `untagged`: untagged.String()}
		logrus.WithFields(fields).Debug("Image removal completed")
	}

	return nil
}

// warnOnHeadFailed decides whether to warn about failed HEAD requests during image pulls.
// It returns true if a warning should be logged, based on the configured strategy.
func (c imageClient) warnOnHeadFailed(targetContainer types.Container, warnOnHeadFailed WarningStrategy) bool {
	if warnOnHeadFailed == WarnAlways {
		return true
	}

	if warnOnHeadFailed == WarnNever {
		return false
	}

	return registry.WarnOnAPIConsumption(targetContainer)
}
