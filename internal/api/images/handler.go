package images

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"

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

// Handler serves the /v1/images endpoint.
type Handler struct {
	list ListFunc
	Path string
}

// New creates a new images handler backed by the given list function.
func New(list ListFunc) *Handler {
	return &Handler{
		list: list,
		Path: "/v1/images",
	}
}

// Handle responds with the JSON status of every tracked image.
//
//	@Summary		List tracked images
//	@Description	Returns the current image identity and digest for every image tracked by Watchtower. Optionally filter by image name or image ID.
//	@Tags			images
//	@Accept			json
//	@Produce		json
//	@Param			name	query		string					false	"Filter by image name (exact match)"
//	@Param			id		query		string					false	"Filter by image ID (sha256:...)"
//	@Success		200		{object}	map[string]interface{}	"Image statuses with count and timestamp"
//	@Failure		500		{string}	string					"Failed to list images"
//	@Router			/v1/images [get]
func (h *Handler) Handle(c fiber.Ctx) error {
	logrus.WithFields(logrus.Fields{
		"method": c.Method(),
		"path":   c.Path(),
	}).Debug("Received HTTP API images request")

	statuses, err := h.list(c.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to list images for API")

		sendErr := c.Status(fiber.StatusInternalServerError).SendString("failed to list images")
		if sendErr != nil {
			return fmt.Errorf("failed to send error response: %w", sendErr)
		}

		return fiber.ErrInternalServerError
	}

	nameFilter := c.Query("name")
	idFilter := c.Query("id")

	if nameFilter != "" || idFilter != "" {
		statuses = filterImages(statuses, nameFilter, idFilter)
	}

	err = c.Status(fiber.StatusOK).JSON(fiber.Map{
		"images":      statuses,
		"count":       len(statuses),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}

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
func ListImageStatuses(ctx context.Context, client container.Client, filter types.Filter) ([]ImageStatus, error) {
	list, err := listContainers(ctx, client, filter)
	if err != nil {
		return nil, err
	}

	imageMap := make(map[string]*ImageStatus)

	for _, c := range list {
		imageName := c.ImageName()
		imageID := string(c.ImageID())

		key := imageName
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
			_, digest, found := strings.Cut(info.RepoDigests[0], "@")
			if found {
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
