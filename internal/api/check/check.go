package check

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ContainerCheck holds the update availability result for a single container.
type ContainerCheck struct {
	Name            string    `json:"name"`
	Image           string    `json:"image"`
	ImageID         string    `json:"image_id"`
	Digest          string    `json:"digest"`
	UpdateAvailable bool      `json:"update_available"`
	LatestImageID   string    `json:"latest_image_id"`
	LatestDigest    string    `json:"latest_digest"`
	Error           string    `json:"error,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// CheckFunc performs the update availability check for all watched containers.
type CheckFunc func(ctx context.Context, images, names []string) ([]ContainerCheck, error)

// Handler serves the /v1/check endpoint.
type Handler struct {
	check CheckFunc
	Path  string
}

// New creates a new check handler backed by the given check function.
func New(check CheckFunc) *Handler {
	return &Handler{
		check: check,
		Path:  "/v1/check",
	}
}

// Handle processes HTTP check requests. It extracts filter parameters, runs
// the check function, and returns JSON results.
//
//	@Summary		Check for available container updates
//	@Description	Checks each watched container for available image updates without pulling or restarting. Returns per-container update availability.
//	@Tags			check
//	@Accept			json
//	@Produce		json
//	@Param			image		query		string					false	"Filter by image name (comma-separated, repeatable)"
//	@Param			container	query		string					false	"Filter by container name (comma-separated, repeatable)"
//	@Success		200			{object}	map[string]interface{}	"Container update availability results"
//	@Failure		500			{string}	string					"Failed to check for updates"
//	@Router			/v1/check [post]
func (h *Handler) Handle(c fiber.Ctx) error {
	logrus.WithFields(logrus.Fields{
		"method": c.Method(),
		"path":   c.Path(),
	}).Info("Received HTTP API check request")

	images := extractFilterParams(c, "image")
	containers := extractFilterParams(c, "container")

	results, err := h.check(c.Context(), images, containers)
	if err != nil {
		logrus.WithError(err).Error("Failed to check for updates")

		sendErr := c.Status(fiber.StatusInternalServerError).SendString("failed to check for updates")
		if sendErr != nil {
			return fmt.Errorf("failed to send error response: %w", sendErr)
		}

		return fiber.ErrInternalServerError
	}

	err = c.Status(fiber.StatusOK).JSON(fiber.Map{
		"containers":  results,
		"count":       len(results),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}

// extractFilterParams parses comma-separated query parameters into a slice.
// Supports both repeated params (?name=a&name=b) and comma-separated values (?name=a,b).
func extractFilterParams(c fiber.Ctx, key string) []string {
	var results []string

	queryArgs := c.Request().URI().QueryArgs()
	values := queryArgs.PeekMulti(key)

	for _, v := range values {
		parts := strings.SplitSeq(string(v), ",")
		for p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				results = append(results, trimmed)
			}
		}
	}

	return results
}

// CheckForUpdates iterates all watched containers and checks each for updates.
// It uses IsContainerStale to compare the current image against the registry.
// The images and names parameters act as filters — if non-empty, only containers
// matching at least one pattern are checked.
func CheckForUpdates(
	ctx context.Context,
	client container.Client,
	filter types.Filter,
	images []string,
	names []string,
) ([]ContainerCheck, error) {
	containers, err := listContainers(ctx, client, filter)
	if err != nil {
		return nil, err
	}

	results := make([]ContainerCheck, 0, len(containers))
	now := time.Now().UTC()

	for _, c := range containers {
		if !matchesFilters(c, images, names) {
			continue
		}

		result := ContainerCheck{
			Name:      c.Name(),
			Image:     c.ImageName(),
			ImageID:   string(c.ImageID()),
			Timestamp: now,
		}

		if info := c.ImageInfo(); info != nil && len(info.RepoDigests) > 0 {
			_, digest, found := strings.Cut(info.RepoDigests[0], "@")
			if found {
				result.Digest = digest
			}
		}

		stale, latestID, err := client.IsContainerStale(ctx, c, types.UpdateParams{})
		if err != nil {
			result.Error = err.Error()

			logrus.WithError(err).WithFields(logrus.Fields{
				"container": c.Name(),
				"image":     c.ImageName(),
			}).Debug("Failed to check container for updates")
		} else {
			result.UpdateAvailable = stale
			result.LatestImageID = string(latestID)
		}

		results = append(results, result)
	}

	return results, nil
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
		return nil, fmt.Errorf("failed to list containers for check: %w", err)
	}

	return list, nil
}

func matchesFilters(c types.Container, images, names []string) bool {
	if len(images) == 0 && len(names) == 0 {
		return true
	}

	if slices.Contains(images, c.ImageName()) {
		return true
	}

	return slices.Contains(names, c.Name())
}
