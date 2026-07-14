package check

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Handler serves the /v1/check endpoint.
type Handler struct {
	check            CheckFunc
	Path             string
	maxTimeout       time.Duration
	notifier         types.Notifier
	splitByContainer bool
}

// New creates a new check handler backed by the given check function.
//
// Parameters:
//   - check: Function that checks container update availability.
//   - maxTimeout: Maximum allowed per-request timeout, used to bound the ?timeout= query parameter.
//   - notifier: Optional notification system instance. When provided, notification batching is enabled
//     during the check to prevent log entries from triggering immediate notifications.
//   - splitByContainer: When true, notifications are split by container instead of being grouped.
func New(check CheckFunc, maxTimeout time.Duration, notifier types.Notifier, splitByContainer bool) *Handler {
	return &Handler{
		check:            check,
		Path:             "/v1/check",
		maxTimeout:       maxTimeout,
		notifier:         notifier,
		splitByContainer: splitByContainer,
	}
}

// Handle processes HTTP check requests. It extracts filter parameters, runs
// the check function, and returns JSON results. If no check function is
// configured, it returns a 500 response.
//
//	@Summary		Check for available container updates
//	@Description	Checks each watched container for available updates by querying the registry for the latest digest without pulling image layers.
//
//	@Tags			check
//	@Accept			json
//	@Produce		json
//	@Param			image		query		string					false	"Image names to check (comma-separated, repeatable). When combined with container, only containers matching both are checked."
//	@Param			container	query		string					false	"Container names to check (comma-separated, repeatable). When combined with image, only containers matching both are checked."
//	@Param			timeout		query		string					false	"Per-request timeout override (e.g. 30s, 2m). Bounded by the configured check API timeout."
//	@Success		200			{object}	map[string]interface{}	"Container update availability results"
//	@Failure		500			{string}	string					"Failed to check for updates"
//	@Failure		401			{string}	string					"Missing or invalid API token"
//	@Security		BearerAuth
//	@Router		/v1/check [post]
func (h *Handler) Handle(c fiber.Ctx) error {
	if h.check == nil {
		logrus.WithFields(logrus.Fields{
			"notify": "no",
		}).Warn("Received HTTP API check request but no check function is configured")

		sendErr := c.Status(fiber.StatusInternalServerError).
			SendString("check function is not configured")
		if sendErr != nil {
			return fmt.Errorf("failed to send error response: %w", sendErr)
		}

		return nil
	}

	logrus.WithFields(logrus.Fields{
		"method": c.Method(),
		"path":   c.Path(),
		"notify": "no",
	}).Info("Received HTTP API check request")

	images := extractFilterParams(c, "image")
	containers := extractFilterParams(c, "container")

	ctx := c.Context()
	if timeoutStr := c.Query("timeout"); timeoutStr != "" {
		parsed, err := time.ParseDuration(timeoutStr)
		if err == nil && parsed > 0 {
			if parsed > h.maxTimeout {
				parsed = h.maxTimeout
			}

			var cancel func()

			ctx, cancel = context.WithTimeout(ctx, parsed)
			defer cancel()
		}
	}

	// Suppress notifications during the check to prevent log entries from
	// triggering immediate notifications. The notifier is reset after the check.
	if h.notifier != nil {
		h.notifier.StartNotification(true)

		defer func() {
			if h.splitByContainer {
				sendSplitCheckNotifications(h.notifier)
				h.notifier.StartNotification(true)
			} else {
				h.notifier.SendNotification(nil)
			}

			h.notifier.StartNotification(false)
		}()
	}

	results, err := h.check(ctx, images, containers)
	if err != nil {
		logrus.WithError(err).WithField("notify", "no").
			Error("Failed to check for updates")

		sendErr := c.Status(fiber.StatusInternalServerError).
			SendString("failed to check for updates")
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

// sendSplitCheckNotifications sends per-container notifications when
// split-by-container is enabled for the check endpoint.
// It groups queued log entries by container name and sends one notification
// per container.
func sendSplitCheckNotifications(notifier types.Notifier) {
	entries := notifier.GetEntries()
	if len(entries) == 0 {
		return
	}

	byContainer := make(map[string][]*logrus.Entry)

	for _, entry := range entries {
		container, _ := entry.Data["container"].(string)
		if container == "" {
			container = "unknown"
		}

		byContainer[container] = append(byContainer[container], entry)
	}

	for _, containerEntries := range byContainer {
		if len(containerEntries) > 0 {
			notifier.SendFilteredEntries(containerEntries, nil)
		}
	}
}
