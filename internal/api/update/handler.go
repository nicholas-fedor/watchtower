package update

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/metrics"
)

// Handler triggers container update scans via HTTP.
type Handler struct {
	fn   func(ctx context.Context, images, containers []string) *metrics.Metric
	Path string
	lock chan bool
}

// lockResult holds the outcome of an acquireLock attempt.
type lockResult struct {
	Token      bool
	Acquired   bool
	RequestErr bool
}

const (
	// retryAfterSeconds is the value for the Retry-After header in 429 responses.
	retryAfterSeconds = "30"
)

// New creates a new Handler.
//
// Parameters:
//   - updateFn: Function that executes container updates, accepting a context,
//     image names, container name patterns, and returning metrics.
//   - updateLock: Optional lock channel for synchronizing updates. If nil, a
//     new channel is created.
func New(updateFn func(ctx context.Context, images, containers []string) *metrics.Metric, updateLock chan bool) *Handler {
	var hLock chan bool
	if updateLock != nil {
		hLock = updateLock

		logrus.WithField("source", "provided").Debug("Initialized update lock from provided channel")
	} else {
		hLock = make(chan bool, 1)
		hLock <- true

		logrus.Debug("Initialized new update lock channel")
	}

	return &Handler{
		fn:   updateFn,
		Path: "/v1/update",
		lock: hLock,
	}
}

// Handle processes an HTTP update request, extracting image and container
// targets from query parameters and dispatching to async or sync execution.
//
//	@Summary		Trigger container update scan
//	@Description	Scans watched containers for image updates and applies them. Supports both full scans and targeted updates filtered by image name or container name. Container patterns support Go
//
// regex syntax.
//
//	@Tags			update
//	@Accept			json
//	@Produce		json
//	@Param			image		query		string					false	"Comma-separated image names to update (repeatable)"
//	@Param			container	query		string					false	"Comma-separated container name patterns to update (repeatable, supports Go regex)"
//	@Param			async		query		string					false	"When 'true', runs update asynchronously and returns 202 Accepted"
//	@Success		200			{object}	map[string]interface{}	"Synchronous update results with summary and timing"
//	@Success		202			{string}	string					"Asynchronous update accepted"
//	@Failure		429			{string}	string					"Another update is already running"
//	@Header			429			{string}	Retry-After				"Seconds to wait before retrying"
//	@Failure		503			{string}	string					"Request cancelled while waiting for lock"
//	@Router			/v1/update [post]
func (h *Handler) Handle(c fiber.Ctx) error {
	logrus.WithFields(logrus.Fields{
		"method": c.Method(),
		"path":   c.Path(),
	}).Info("Received HTTP API update request")

	images := h.extractImages(c)
	containers := h.extractContainers(c)

	result := h.acquireLock(c, images, containers)
	if result.RequestErr {
		return fiber.ErrServiceUnavailable
	}

	if !result.Acquired {
		return nil
	}

	if c.Query("async") == "true" {
		return h.handleAsync(c, images, containers, result.Token)
	}

	return h.handleSync(c, images, containers, result.Token)
}

// extractImages parses the "image" query parameters into a slice of image
// strings. It supports comma-separated values within a single query parameter
// and multiple "image" parameters (e.g., ?image=a&image=b or ?image=a,b).
// Empty values are filtered out.
func (h *Handler) extractImages(c fiber.Ctx) []string {
	var images []string

	queryArgs := c.Request().URI().QueryArgs()
	values := queryArgs.PeekMulti("image")

	for _, v := range values {
		parts := strings.SplitSeq(string(v), ",")
		for p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				images = append(images, trimmed)
			}
		}
	}

	if len(images) > 0 {
		logrus.WithField("images", images).Debug("Extracted images from query parameters")
	} else {
		logrus.Debug("No image query parameters provided")
	}

	return images
}

// extractContainers parses the "container" query parameters into a slice of
// container name patterns. Supports comma-separated values and repeated params.
// Empty values are filtered out.
func (h *Handler) extractContainers(c fiber.Ctx) []string {
	var containers []string

	queryArgs := c.Request().URI().QueryArgs()
	values := queryArgs.PeekMulti("container")

	for _, v := range values {
		parts := strings.SplitSeq(string(v), ",")
		for p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				containers = append(containers, trimmed)
			}
		}
	}

	if len(containers) > 0 {
		logrus.WithField("containers", containers).Debug("Extracted container patterns from query parameters")
	} else {
		logrus.Debug("No container query parameters provided")
	}

	return containers
}

// acquireLock attempts to acquire the update lock.
//
// For targeted updates (len(images) > 0 or len(containers) > 0), it blocks
// until the lock is available or the request is cancelled. For full updates,
// it attempts a non-blocking acquire and returns a 429 response if the lock
// is held.
func (h *Handler) acquireLock(c fiber.Ctx, images, containers []string) lockResult {
	logrus.Debug("Handler: trying to acquire lock")

	if len(images) > 0 || len(containers) > 0 {
		select {
		case token := <-h.lock:
			logrus.Debug("Handler: acquired lock for targeted update")

			return lockResult{Token: token, Acquired: true}
		case <-c.Context().Done():
			logrus.Debug("Handler: request cancelled while waiting for lock")

			return lockResult{RequestErr: true}
		}
	}

	select {
	case token := <-h.lock:
		logrus.Debug("Handler: acquired lock for full update")

		return lockResult{Token: token, Acquired: true}
	default:
		logrus.Debug("Skipped update, another update already in progress")
		h.send429Response(c)

		return lockResult{}
	}
}

// send429Response writes a JSON error response indicating an update is
// already running.
func (h *Handler) send429Response(c fiber.Ctx) {
	c.Set("Retry-After", retryAfterSeconds)

	err := c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
		"error":       "another update is already running",
		"api_version": "v1",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		logrus.WithError(err).Debug("Failed to send 429 response")
	}
}

// handleAsync processes an asynchronous update request by spawning a
// goroutine and returning 202 Accepted.
func (h *Handler) handleAsync(c fiber.Ctx, images, containers []string, lockToken bool) error {
	logrus.Info("Handling async update request - spawning async update")

	go h.executeUpdateAsync(c.Context(), images, containers, lockToken)

	err := c.SendStatus(fiber.StatusAccepted)
	if err != nil {
		return fmt.Errorf("failed to send 202 response: %w", err)
	}

	return nil
}

// handleSync processes a synchronous update request, returning the update
// results as JSON.
func (h *Handler) handleSync(c fiber.Ctx, images, containers []string, lockToken bool) error {
	defer h.releaseLock(lockToken)

	metric, duration := h.executeUpdate(c.Context(), images, containers)
	if metric == nil {
		return fiber.ErrInternalServerError
	}

	err := c.Status(fiber.StatusOK).JSON(fiber.Map{
		"summary": fiber.Map{
			"scanned":   metric.Scanned,
			"updated":   metric.Updated,
			"failed":    metric.Failed,
			"restarted": metric.Restarted,
			"skipped":   metric.Skipped,
		},
		"timing": fiber.Map{
			"duration_ms": duration.Milliseconds(),
			"duration":    duration.String(),
		},
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}

// executeUpdateAsync runs the update function in a goroutine, ensuring the
// lock is released when done.
func (h *Handler) executeUpdateAsync(ctx context.Context, images, containers []string, lockToken bool) {
	defer func() {
		if rec := recover(); rec != nil {
			logrus.WithField("panic", rec).Error("Update goroutine panicked")
		}

		h.releaseLock(lockToken)
	}()

	startTime := time.Now()

	h.fn(ctx, images, containers)

	duration := time.Since(startTime)
	logrus.WithField("duration", duration).Debug("Handler (async): update function completed")
}

// executeUpdate runs the update function and returns the metric along with
// duration.
func (h *Handler) executeUpdate(ctx context.Context, images, containers []string) (*metrics.Metric, time.Duration) {
	logrus.Debug("Handler: executing update function")

	startTime := time.Now()
	metric := h.fn(ctx, images, containers)
	duration := time.Since(startTime)

	logrus.Debug("Handler: update function completed")

	return metric, duration
}

// releaseLock returns the lock token to the channel, allowing another update
// to proceed.
func (h *Handler) releaseLock(token bool) {
	logrus.Debug("Handler: releasing lock")

	h.lock <- token
}
