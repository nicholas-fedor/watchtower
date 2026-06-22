package containers

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
)

// Handler serves the /v1/containers endpoint.
type Handler struct {
	list ListFunc
	Path string
}

// New creates a new containers Handler backed by the given list function.
//
// Parameters:
//   - list: Function that returns the current status of all watched containers.
func New(list ListFunc) *Handler {
	return &Handler{
		list: list,
		Path: "/v1/containers",
	}
}

// Handle responds with the JSON status of every watched container.
//
//	@Summary		List watched container statuses
//	@Description	Returns the current image identity and digest for every watched container. Optionally filter by container name or image name.
//	@Tags			containers
//	@Accept			json
//	@Produce		json
//	@Param			name	query		string					false	"Filter by container name (exact match)"
//	@Param			image	query		string					false	"Filter by image name (exact match)"
//	@Success		200		{object}	map[string]interface{}	"Container statuses with count and timestamp"
//	@Failure		500		{string}	string					"Failed to list containers"
//	@Router			/v1/containers [get]
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

	nameFilter := c.Query("name")
	imageFilter := c.Query("image")

	if nameFilter != "" || imageFilter != "" {
		statuses = filterStatuses(statuses, nameFilter, imageFilter)
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
