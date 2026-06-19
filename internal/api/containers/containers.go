package containers

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

// Status describes a single watched container's current image identity.
type Status struct {
	// Name is the container name.
	Name string `json:"name"`
	// Image is the image reference with tag (e.g. ethpandaops/lighthouse:latest).
	Image string `json:"image"`
	// ImageID is the local image config ID (sha256:...).
	ImageID string `json:"image_id"`
	// Digest is the registry manifest digest the image was pulled from
	// (sha256:...), derived from the image's RepoDigests. It is directly
	// comparable to a registry's Docker-Content-Digest. Empty for locally-built
	// images with no registry reference.
	Digest string `json:"digest"`
}

// ListFunc returns the current status of all watched containers.
type ListFunc func(ctx context.Context) ([]Status, error)

// Handler serves the /v1/containers endpoint.
type Handler struct {
	list ListFunc
	Path string
}

// New creates a new containers Handler backed by the given list function.
func New(list ListFunc) *Handler {
	return &Handler{
		list: list,
		Path: "/v1/containers",
	}
}

// Handle responds with the JSON status of every watched container.
func (h *Handler) Handle(c fiber.Ctx) error {
	logrus.WithFields(logrus.Fields{
		"method": c.Method(),
		"path":   c.Path(),
	}).Debug("Received HTTP API containers request")

	statuses, err := h.list(c.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to list containers for API")

		sendErr := c.Status(fiber.StatusInternalServerError).SendString("failed to list containers")
		if sendErr != nil {
			return fmt.Errorf("failed to send error response: %w", sendErr)
		}

		return fiber.ErrInternalServerError
	}

	err = c.Status(fiber.StatusOK).JSON(fiber.Map{
		"containers":  statuses,
		"count":       len(statuses),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}

// ListContainerStatuses fetches all containers from the client and transforms
// them into API status objects with image identity information.
func ListContainerStatuses(ctx context.Context, client container.Client, filter types.Filter) ([]Status, error) {
	list, err := listContainers(ctx, client, filter)
	if err != nil {
		return nil, err
	}

	statuses := make([]Status, 0, len(list))
	for _, c := range list {
		statuses = append(statuses, containerToStatus(c))
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
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	return list, nil
}

func containerToStatus(c types.Container) Status {
	status := Status{
		Name:    c.Name(),
		Image:   c.ImageName(),
		ImageID: string(c.ImageID()),
	}

	if info := c.ImageInfo(); info != nil {
		status.Digest = extractDigest(info.RepoDigests, c.Name())
	}

	return status
}

func extractDigest(repoDigests []string, containerName string) string {
	if len(repoDigests) == 0 {
		return ""
	}

	_, digest, found := strings.Cut(repoDigests[0], "@")
	if found {
		return digest
	}

	logrus.WithFields(logrus.Fields{
		"container": containerName,
		"digest":    repoDigests[0],
	}).Debug("RepoDigest in unexpected format, missing @ separator")

	return ""
}
