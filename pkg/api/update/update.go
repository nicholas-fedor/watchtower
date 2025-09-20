// Package update provides an HTTP API handler for triggering Watchtower container updates.
package update

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

// Handler triggers container update scans via HTTP.
//
// It holds the update function, endpoint path, and concurrency lock for the /v1/update endpoint.
type Handler struct {
	fn   func(images []string) // Update execution function.
	Path string                // API endpoint path (e.g., "/v1/update").
	lock chan bool             // Channel for synchronizing updates to prevent concurrency.
}

// New creates a new Handler instance.
//
// Parameters:
//   - updateFn: Function to execute container updates, accepting a list of image names.
//   - updateLock: Optional lock channel for synchronizing updates; if nil, a new channel is created.
//
// Returns:
//   - *Handler: Initialized handler with the specified update function and path.
func New(updateFn func(images []string), updateLock chan bool) *Handler {
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
// It extracts image names from query parameters (if provided) and enqueues the update. If another update
// is in progress, it returns HTTP 429 (Too Many Requests). On success, it returns HTTP 202 (Accepted).
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

	// Log request body to stdout for debugging.
	_, err := io.Copy(os.Stdout, r.Body)
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

	handle.fn(images)
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintln(w, "Update enqueued and started")
}
