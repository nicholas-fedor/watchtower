package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

// retryAfterSeconds is the value for the Retry-After header in 429 responses.
const retryAfterSeconds = "30"

// maxRequestBodySize defines the maximum request body size (1 MiB) to prevent
// resource exhaustion from large uploads.
const maxRequestBodySize = 1 << 20

// Handler triggers container update scans via HTTP.
//
// It holds the update function, endpoint path, and concurrency lock for the /v1/update endpoint.
type Handler struct {
	fn   func(images []string) *metrics.Metric // Update execution function.
	Path string                                // API endpoint path (e.g., "/v1/update").
	lock chan bool                             // Channel for synchronizing updates to prevent concurrency.
}

// New creates a new Handler instance.
//
// This factory function initializes a Handler with the provided update function and an optional
// lock channel. If no lock channel is provided, a new buffered channel (capacity 1) is created
// and seeded with a token to represent an initially-unlocked state.
//
// Parameters:
//   - updateFn: Function to execute container updates, accepting a list of image names and returning metrics.
//   - updateLock: Optional lock channel for synchronizing updates; if nil, a new channel is created.
//
// Returns:
//   - *Handler: Initialized handler with the specified update function and path.
func New(updateFn func(images []string) *metrics.Metric, updateLock chan bool) *Handler {
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

// Handle processes HTTP update requests, triggering container updates with lock synchronization.
//
// For HEAD requests, it triggers the update asynchronously and returns immediately with HTTP 202 Accepted.
// For targeted updates (with image query parameters), the handler blocks until the lock is available.
// For full updates (no image query parameters), the handler returns HTTP 429 if another update is running.
// On success (POST), it returns HTTP 200 with JSON results including summary metrics, timing, and metadata.
//
// Parameters:
//   - w: HTTP response writer for sending status codes and responses.
//   - r: HTTP request containing optional "image" query parameters for targeted updates.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API update request")

	if !h.readRequestBody(w, r) {
		return
	}

	images := h.extractImages(r)

	lockToken, acquired, requestErr := h.acquireLock(w, r, images)
	if requestErr {
		return
	}

	if !acquired {
		return // 429 response already sent
	}

	if r.Method == http.MethodHead {
		h.handleHead(w, images, lockToken)

		return
	}

	h.handlePost(w, images, lockToken)
}

// readRequestBody discards the request body up to the maximum allowed size.
// On error, it logs and sends an appropriate HTTP error response.
//
// Parameters:
//   - w: HTTP response writer (used to send error responses).
//   - r: HTTP request containing the body to be discarded.
//
// Returns:
//   - bool: true if the body was read successfully, false otherwise.
func (h *Handler) readRequestBody(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	_, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)

			return false
		}

		logrus.WithError(err).Debug("Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)

		return false
	}

	return true
}

// extractImages parses the "image" query parameters into a slice of image strings.
// It supports comma-separated values within a single query parameter and multiple
// "image" parameters, combining all values into a single slice.
//
// Parameters:
//   - r: HTTP request containing optional "image" query parameters.
//
// Returns:
//   - []string: Slice of image names to update; may be empty if no images specified.
func (h *Handler) extractImages(r *http.Request) []string {
	var images []string

	imageQueries, found := r.URL.Query()["image"]
	if found {
		for _, image := range imageQueries {
			images = append(images, strings.Split(image, ",")...)
		}

		logrus.WithField("images", images).Debug("Extracted images from query parameters")
	} else {
		logrus.Debug("No image query parameters provided")
	}

	return images
}

// acquireLock attempts to acquire the update lock.
//
// For targeted updates (len(images) > 0), it blocks until the lock is available or the request is cancelled.
// For full updates, it attempts a non-blocking acquire and returns false with a 429 response if the lock is held.
//
// Parameters:
//   - w: HTTP response writer (used to send 429 error if lock unavailable for full update).
//   - r: HTTP request (used to check context cancellation for targeted updates).
//   - images: Slice of image names; determines targeted vs full update strategy.
//
// Returns:
//   - lockToken (bool): The lock token to be returned to the channel when releasing.
//   - acquired (bool): true if the lock was acquired, false otherwise.
//   - requestErr (bool): true if an error response was already sent (cancellation), false otherwise.
func (h *Handler) acquireLock(w http.ResponseWriter, r *http.Request, images []string) (bool, bool, bool) {
	logrus.Debug("Handler: trying to acquire lock")

	if len(images) > 0 {
		select {
		case token := <-h.lock:
			logrus.Debug("Handler: acquired lock for targeted update")

			return token, true, false
		case <-r.Context().Done():
			logrus.Debug("Handler: request cancelled while waiting for lock")
			http.Error(w, "request cancelled", http.StatusServiceUnavailable)

			return false, false, true
		}
	}

	select {
	case token := <-h.lock:
		logrus.Debug("Handler: acquired lock for full update")

		return token, true, false
	default:
		logrus.Debug("Skipped update, another update already in progress")
		h.send429Response(w)

		return false, false, false
	}
}

// send429Response writes a JSON error response indicating an update is already running.
// It sets the Retry-After header to suggest when the client may retry.
//
// Parameters:
//   - w: HTTP response writer to send the error payload.
func (h *Handler) send429Response(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", retryAfterSeconds)
	w.WriteHeader(http.StatusTooManyRequests)

	errResponse := map[string]any{
		"error":       "another update is already running",
		"api_version": "v1",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}

	var buf bytes.Buffer

	err := json.NewEncoder(&buf).Encode(errResponse)
	if err != nil {
		logrus.WithError(err).Error("Failed to encode 429 response")

		return
	}

	_, err = w.Write(buf.Bytes())
	if err != nil {
		logrus.WithError(err).Error("Failed to write 429 response")
	}
}

// handleHead processes a HEAD request by spawning an asynchronous update and returning 202 Accepted.
// The update runs in a separate goroutine, allowing the client to fire-and-forget.
//
// Parameters:
//   - w: HTTP response writer to send the 202 Accepted response.
//   - images: Slice of image names to update (passed to the async update function).
//   - lockToken: The lock token to be released by the async goroutine upon completion.
func (h *Handler) handleHead(w http.ResponseWriter, images []string, lockToken bool) {
	logrus.Info("Handling HEAD request - spawning async update")

	go func() {
		h.executeUpdateAsync(images, lockToken)
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
}

// handlePost processes a POST request synchronously, returning the update results as JSON.
// It defers lock release until the update completes and response is written.
//
// Parameters:
//   - w: HTTP response writer to send the 200 OK response with JSON body.
//   - images: Slice of image names to update.
//   - lockToken: The lock token to be released via defer upon function return.
func (h *Handler) handlePost(w http.ResponseWriter, images []string, lockToken bool) {
	defer h.releaseLock(lockToken)

	metric, duration := h.executeUpdate(images)
	h.writeSuccessResponse(w, metric, duration)
}

// executeUpdateAsync runs the update function in a goroutine, ensuring the lock is released when done.
// It recovers from panics to avoid crashing the process and logs the completion duration.
//
// Parameters:
//   - images: Slice of image names to update.
//   - lockToken: The lock token to release when the update finishes (or panics).
func (h *Handler) executeUpdateAsync(images []string, lockToken bool) {
	defer func() {
		if rec := recover(); rec != nil {
			logrus.WithField("panic", rec).Error("Update goroutine panicked")
		}

		h.releaseLock(lockToken)
	}()

	startTime := time.Now()

	h.fn(images)

	duration := time.Since(startTime)
	logrus.WithField("duration", duration).Debug("Handler (HEAD): update function completed")
}

// executeUpdate runs the update function and returns the metric along with duration.
//
// Parameters:
//   - images: Slice of image names to update.
//
// Returns:
//   - *metrics.Metric: The update metrics returned by the update function.
//   - time.Duration: The elapsed time taken to execute the update.
func (h *Handler) executeUpdate(images []string) (*metrics.Metric, time.Duration) {
	logrus.Debug("Handler: executing update function")

	startTime := time.Now()
	metric := h.fn(images)
	duration := time.Since(startTime)

	logrus.Debug("Handler: update function completed")

	return metric, duration
}

// writeSuccessResponse encodes the metric into JSON and writes a 200 OK response.
//
// Parameters:
//   - w: HTTP response writer to send the success payload.
//   - metric: The update metrics to encode into the response body.
//   - duration: The elapsed time of the update operation, included in the response timing.
func (h *Handler) writeSuccessResponse(w http.ResponseWriter, metric *metrics.Metric, duration time.Duration) {
	response := map[string]any{
		"summary": map[string]any{
			"scanned":   metric.Scanned,
			"updated":   metric.Updated,
			"failed":    metric.Failed,
			"restarted": metric.Restarted,
			"skipped":   metric.Skipped,
		},
		"timing": map[string]any{
			"duration_ms": duration.Milliseconds(),
			"duration":    duration.String(),
		},
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	}

	var buf bytes.Buffer

	err := json.NewEncoder(&buf).Encode(response)
	if err != nil {
		logrus.WithError(err).Error("Failed to encode JSON response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, err = w.Write(buf.Bytes())
	if err != nil {
		logrus.WithError(err).Error("Failed to write response")
	}
}

// releaseLock returns the lock token to the channel, allowing another update to proceed.
//
// Parameters:
//   - token: The lock token (bool) to send back to the lock channel.
func (h *Handler) releaseLock(token bool) {
	logrus.Debug("Handler: releasing lock")

	h.lock <- token
}
