// Package update provides an HTTP API handler for triggering Watchtower container updates.
package update

import (
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

var lock chan bool

// Handler triggers container update scans via HTTP.
//
// It holds the update function and endpoint path.
type Handler struct {
	fn   func(images []string) // Update execution function.
	Path string                // API endpoint path.
}

// New creates a new Handler instance.
//
// Parameters:
//   - updateFn: Function to execute updates.
//   - updateLock: Optional lock channel for concurrency.
//
// Returns:
//   - *Handler: Initialized handler.
func New(updateFn func(images []string), updateLock chan bool) *Handler {
	// Use provided lock or create a new one.
	if updateLock != nil {
		lock = updateLock

		logrus.WithField("source", "provided").
			Debug("Initialized update lock from provided channel")
	} else {
		lock = make(chan bool, 1)
		lock <- true

		logrus.Debug("Initialized new update lock channel")
	}

	return &Handler{
		fn:   updateFn,
		Path: "/v1/update",
	}
}

// Handle processes HTTP update requests.
//
// Parameters:
//   - w: HTTP response writer (unused here).
//   - r: HTTP request with optional image queries.
func (handle *Handler) Handle(_ http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API update request")

	// Log request body to stdout.
	_, err := io.Copy(os.Stdout, r.Body)
	if err != nil {
		logrus.WithError(err).Debug("Failed to read request body")

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

	// Execute update with lock handling.
	if len(images) > 0 {
		chanValue := <-lock
		defer func() { lock <- chanValue }()
		logrus.WithField("images", images).Info("Executing targeted update")
		handle.fn(images)
	} else {
		select {
		case chanValue := <-lock:
			defer func() { lock <- chanValue }()
			logrus.Info("Executing full update")
			handle.fn(images)
		default:
			logrus.Debug("Skipped update due to concurrent operation")
		}
	}
}
