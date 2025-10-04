// Package update provides an HTTP API handler for triggering Watchtower container updates.
package update

import (
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
// It extracts image names from query parameters (if provided) and executes the update. If another update
// is in progress, it returns HTTP 429 (Too Many Requests). On success, it returns HTTP 200 (OK) with JSON results.
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
		images = nil

		logrus.Debug("No image query parameters provided")
	}

	// Acquire lock, blocking if another update is in progress (requests will queue).
	chanValue := <-handle.lock

	defer func() { handle.lock <- chanValue }()

	if len(images) > 0 {
		logrus.WithField("images", images).Info("Executing targeted update")
	} else {
		logrus.Info("Executing full update")
	}

	// Execute update and get results
	startTime := time.Now()
	metric := handle.fn(images)
	duration := time.Since(startTime)

	// Set content type to JSON
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Return enhanced JSON response with detailed update results
	response := map[string]any{
		"summary": map[string]any{
			"scanned": metric.Scanned,
			"updated": metric.Updated,
			"failed":  metric.Failed,
		},
		"timing": map[string]any{
			"duration_ms": duration.Milliseconds(),
			"duration":    duration.String(),
		},
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Error("Failed to encode JSON response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}
}
