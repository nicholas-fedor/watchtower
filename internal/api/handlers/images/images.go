package images

import (
	"context"
	"fmt"
	"strings"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ImageStatus describes a tracked image and its current state.
type ImageStatus struct {
	// Name is the image name with tag (e.g. nginx:latest).
	Name string `json:"name"`
	// ImageID is the local image config ID (sha256:...).
	ImageID string `json:"image_id"`
	// Digest is the registry manifest digest (sha256:...).
	Digest string `json:"digest"`
	// Containers is the number of watched containers using this image.
	Containers int `json:"containers"`
}

// ListFunc returns the current status of all tracked images.
type ListFunc func(ctx context.Context) ([]ImageStatus, error)

func filterImages(statuses []ImageStatus, name, imageID string) []ImageStatus {
	var filtered []ImageStatus

	for _, status := range statuses {
		if name != "" && status.Name != name {
			continue
		}

		if imageID != "" && status.ImageID != imageID {
			continue
		}

		filtered = append(filtered, status)
	}

	return filtered
}

// ListImageStatuses aggregates containers by image and returns image statuses.
//
// Parameters:
//   - ctx: Context for the Docker API call.
//   - client: Docker client.
//   - filter: Container filter function.
//
// Returns:
//   - []ImageStatus: Status for each tracked image.
//   - error: Non-nil if listing containers fails.
func ListImageStatuses(ctx context.Context, client container.Client, filter types.Filter) ([]ImageStatus, error) {
	list, err := listContainers(ctx, client, filter)
	if err != nil {
		return nil, err
	}

	imageMap := make(map[string]*ImageStatus)

	for _, c := range list {
		imageName := c.ImageName()
		imageID := string(c.ImageID())

		key := imageID + "|" + imageName
		if existing, ok := imageMap[key]; ok {
			existing.Containers++

			continue
		}

		status := &ImageStatus{
			Name:       imageName,
			ImageID:    imageID,
			Containers: 1,
		}

		if info := c.ImageInfo(); info != nil && len(info.RepoDigests) > 0 {
			if _, digest, found := strings.Cut(info.RepoDigests[0], "@"); found {
				status.Digest = digest
			}
		}

		imageMap[key] = status
	}

	statuses := make([]ImageStatus, 0, len(imageMap))
	for _, s := range imageMap {
		statuses = append(statuses, *s)
	}

	return statuses, nil
}

func listContainers(ctx context.Context, client container.Client, filter types.Filter) ([]types.Container, error) {
	var (
		list []types.Container
		err  error
	)

	if filter != nil {
		list, err = client.ListContainers(ctx, filter)
	} else {
		list, err = client.ListContainers(ctx)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list containers for images: %w", err)
	}

	return list, nil
}
