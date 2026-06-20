package history

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

// errInvalidTimeParameter is returned when a time parameter cannot be parsed.
var errInvalidTimeParameter = errors.New("invalid time parameter")

// Handler serves the /v1/history endpoint.
type Handler struct {
	Path    string
	getHist func(*time.Time, *time.Time, int) []metrics.HistoryEntry
}

// New creates a new history handler backed by the given function.
func New(getHist func(*time.Time, *time.Time, int) []metrics.HistoryEntry) *Handler {
	return &Handler{
		Path:    "/v1/history",
		getHist: getHist,
	}
}

// Handle responds with the scan history as JSON, optionally filtered by
// time range and limited to the most recent N entries.
//
//	@Summary		Scan history
//	@Description	Returns historical scan results from the in-memory ring buffer (up to 500 entries). Optionally filter by time range and limit the number of results.
//	@Tags			history
//	@Accept			json
//	@Produce		json
//	@Param			since	query		string					false	"Include entries at or after this RFC3339 timestamp"
//	@Param			until	query		string					false	"Include entries at or before this RFC3339 timestamp"
//	@Param			limit	query		int						false	"Maximum number of entries to return (default: all)"
//	@Success		200		{object}	map[string]interface{}	"History entries with count and timestamp"
//	@Failure		400		{string}	string					"Invalid query parameter"
//	@Router			/v1/history [get]
func (h *Handler) Handle(c fiber.Ctx) error {
	logrus.WithFields(logrus.Fields{
		"method": c.Method(),
		"path":   c.Path(),
	}).Debug("Received HTTP API history request")

	since, sinceErr := parseTimeParam(c.Query("since"))
	if sinceErr != nil && !errors.Is(sinceErr, errNoTimeParameter) {
		sendErr := c.Status(fiber.StatusBadRequest).SendString("invalid 'since' parameter: " + sinceErr.Error())
		if sendErr != nil {
			return fmt.Errorf("failed to send error response: %w", sendErr)
		}

		return fiber.ErrBadRequest
	}

	until, untilErr := parseTimeParam(c.Query("until"))
	if untilErr != nil && !errors.Is(untilErr, errNoTimeParameter) {
		sendErr := c.Status(fiber.StatusBadRequest).SendString("invalid 'until' parameter: " + untilErr.Error())
		if sendErr != nil {
			return fmt.Errorf("failed to send error response: %w", sendErr)
		}

		return fiber.ErrBadRequest
	}

	limit, err := parseLimit(c.Query("limit"))
	if err != nil {
		sendErr := c.Status(fiber.StatusBadRequest).SendString("invalid 'limit' parameter: " + err.Error())
		if sendErr != nil {
			return fmt.Errorf("failed to send error response: %w", sendErr)
		}

		return fiber.ErrBadRequest
	}

	entries := h.getHist(since, until, limit)

	err = c.Status(fiber.StatusOK).JSON(fiber.Map{
		"entries":     entries,
		"count":       len(entries),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}

func parseTimeParam(value string) (*time.Time, error) {
	var noTime *time.Time
	if value == "" {
		return noTime, errNoTimeParameter
	}

	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidTimeParameter, err)
	}

	return &t, nil
}

// errNoTimeParameter is returned when no time parameter is provided.
var errNoTimeParameter = errors.New("no time parameter provided")

func parseLimit(value string) (int, error) {
	if value == "" {
		return 0, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 {
		return 0, fmt.Errorf("expected non-negative integer: %w", err)
	}

	return limit, nil
}
