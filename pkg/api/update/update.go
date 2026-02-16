package update

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

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
// Parameters:
//   - updateFn: Function to execute container updates, accepting a list of image names and returning metrics.
//   - updateLock: Optional lock channel for synchronizing updates; if nil, a new channel is created.
//
// Returns:
//   - *Handler: Initialized handler with the specified update function and path.
func New(updateFn func(images []string) *metrics.Metric, updateLock chan bool) *Handler {
	var hLock chan bool
	// Use provided lock or create a new one with capacity 1 for single-update serialization.
	if updateLock != nil {
		hLock = updateLock

		logrus.WithField("source", "provided").
			Debug("Initialized update lock from provided channel")
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
// For targeted updates (with image query parameters), the handler blocks until the lock is available,
// ensuring the specific images are updated even if another update is in progress.
//
// For full updates (no image query parameters), the handler returns HTTP 429 (Too Many Requests) immediately
// if another update is already running, since queuing a redundant full scan provides no benefit.
//
// On success, it returns HTTP 200 (OK) with JSON results including summary metrics, timing, and metadata.
// Errors during request processing (e.g., reading the body) return HTTP 500 (Internal Server Error).
//
// Parameters:
//   - w: HTTP response writer for sending status codes and responses.
//   - r: HTTP request containing optional "image" query parameters for targeted updates.
func (handle *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API update request")

	// Discard request body to prevent I/O blocking in tests and CI environments.
	_, err := io.Copy(io.Discard, r.Body)
	if err != nil {
		logrus.WithError(err).Debug("Failed to read request body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)

		return
	}

	// Extract images from query parameters.
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

	// Acquire lock with different strategies based on update type.
	logrus.Debug("Handler: trying to acquire lock")

	if len(images) > 0 {
		// Targeted update: block until the lock is available to ensure specific images are updated.
		// Use select with request context to avoid goroutine leaks if the client disconnects.
		select {
		case chanValue := <-handle.lock:
			logrus.Debug("Handler: acquired lock for targeted update")

			defer func() {
				logrus.Debug("Handler: releasing lock")

				handle.lock <- chanValue
			}()
		case <-r.Context().Done():
			logrus.Debug("Handler: request cancelled while waiting for lock")
			http.Error(w, "request cancelled", http.StatusServiceUnavailable)

			return
		}

		logrus.WithField("images", images).Info("Executing targeted update")
	} else {
		// Full update: try to acquire lock without blocking.
		// If another update is already running, a redundant full scan is unnecessary.
		select {
		case chanValue := <-handle.lock:
			logrus.Debug("Handler: acquired lock for full update")

			defer func() {
				logrus.Debug("Handler: releasing lock")

				handle.lock <- chanValue
			}()
		default:
			logrus.Debug("Skipped update, another update already in progress")

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)

			errResponse := map[string]any{
				"error":       "another update is already running",
				"api_version": "v1",
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
			}

			var buf bytes.Buffer
			encErr := json.NewEncoder(&buf).Encode(errResponse)
			if encErr != nil {
				logrus.WithError(encErr).Error("Failed to encode 429 response")

				return
			}

			_, writeErr := w.Write(buf.Bytes())
			if writeErr != nil {
				logrus.WithError(writeErr).Error("Failed to write 429 response")
			}

			return
		}

		logrus.Info("Executing full update")
	}

	// Execute update and get results
	logrus.Debug("Handler: executing update function")

	startTime := time.Now()
	metric := handle.fn(images)
	duration := time.Since(startTime)

	logrus.Debug("Handler: update function completed")

	// Return enhanced JSON response with detailed update results
	response := map[string]any{
		"summary": map[string]any{
			"scanned":   metric.Scanned,
			"updated":   metric.Updated,
			"failed":    metric.Failed,
			"restarted": metric.Restarted,
		},
		"timing": map[string]any{
			"duration_ms": duration.Milliseconds(),
			"duration":    duration.String(),
		},
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	}

	var buf bytes.Buffer

	err = json.NewEncoder(&buf).Encode(response)
	if err != nil {
		logrus.WithError(err).Error("Failed to encode JSON response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}

	// Set content type to JSON and write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	_, err = w.Write(buf.Bytes())
	if err != nil {
		logrus.WithError(err).Error("Failed to write response")
	}
}
