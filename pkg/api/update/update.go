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

// New is a factory function creating a new Handler instance.
// It initializes the update lock, either from the provided channel or a new one.
func New(updateFn func(images []string), updateLock chan bool) *Handler {
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

// Handler is an API handler used for triggering container update scans.
// It encapsulates the update function and the API endpoint path.
type Handler struct {
	fn   func(images []string)
	Path string
}

// Handle processes HTTP requests to trigger container updates.
// It reads the request body, extracts image queries, and executes the update function.
func (handle *Handler) Handle(_ http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Info("Received HTTP API update request")

	_, err := io.Copy(os.Stdout, r.Body)
	if err != nil {
		logrus.WithError(err).Debug("Failed to read request body")

		return
	}

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
