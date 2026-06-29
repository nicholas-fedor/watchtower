package check

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
)

// Handler serves the /v1/check endpoint.
type Handler struct {
	check CheckFunc
	Path  string
}

// New creates a new check handler backed by the given check function.
//
// Parameters:
//   - check: Function that checks container update availability.
func New(check CheckFunc) *Handler {
	return &Handler{
		check: check,
		Path:  "/v1/check",
	}
}

// Handle processes HTTP check requests. It extracts filter parameters, runs
// the check function, and returns JSON results. If no check function is
// configured, it returns a 500 response.
//
//	@Summary		Check for available container updates
//	@Description	Checks each watched container for available updates by querying the registry for the latest digest
//
//	@Tags			check
//	@Accept			json
//	@Produce		json
//	@Param			image		query		string					false	"Filter by image name (comma-separated, repeatable)"
//	@Param			container	query		string					false	"Filter by container name (comma-separated, repeatable)"
//	@Success		200			{object}	map[string]interface{}	"Container update availability results"
//	@Failure		500			{string}	string					"Failed to check for updates"
//	@Router			/v1/check [post]
func (h *Handler) Handle(c fiber.Ctx) error {
	if h.check == nil {
		logrus.Warn("Received HTTP API check request but no check function is configured")

		sendErr := c.Status(fiber.StatusInternalServerError).SendString("check function is not configured")
		if sendErr != nil {
			return fmt.Errorf("failed to send error response: %w", sendErr)
		}

		return nil
	}

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

		return nil
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
