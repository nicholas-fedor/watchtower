// Package host provides HTTP API handlers for host system metrics endpoints.
package host

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Handler provides HTTP endpoints for host system information.
type Handler struct {
	Path   string       // Base path for host endpoints
	Client types.Client // Docker client for retrieving host information
}

// New creates a new host metrics handler.
//
// Parameters:
//   - client: Docker client for retrieving system information
//
// Returns:
//   - *Handler: Initialized handler for host metrics endpoints
func New(client types.Client) *Handler {
	return &Handler{
		Path:   "/v1/metrics/host",
		Client: client,
	}
}

// ServeHTTP implements http.Handler interface and routes requests to the appropriate host metrics endpoint based on the URL path.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Debug("Received host metrics request")

	// Extract the endpoint from the path
	endpoint := strings.TrimPrefix(r.URL.Path, h.Path+"/")

	switch endpoint {
	case "info":
		h.handleInfo(w, r)
	case "version":
		h.handleVersion(w, r)
	case "disk-usage":
		h.handleDiskUsage(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// handleInfo returns system information from the Docker daemon.
func (h *Handler) handleInfo(w http.ResponseWriter, _ *http.Request) {
	info, err := h.Client.GetInfo()
	if err != nil {
		logrus.WithError(err).Error("Failed to get system info")
		http.Error(w, "Failed to get system info", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(info); err != nil {
		logrus.WithError(err).Error("Failed to encode system info response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}
}

// handleVersion returns version information from the Docker daemon.
func (h *Handler) handleVersion(w http.ResponseWriter, _ *http.Request) {
	version, err := h.Client.GetServerVersion()
	if err != nil {
		logrus.WithError(err).Error("Failed to get server version")
		http.Error(w, "Failed to get server version", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(version); err != nil {
		logrus.WithError(err).Error("Failed to encode version response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}
}

// handleDiskUsage returns disk usage information from the Docker daemon.
func (h *Handler) handleDiskUsage(w http.ResponseWriter, _ *http.Request) {
	usage, err := h.Client.GetDiskUsage()
	if err != nil {
		logrus.WithError(err).Error("Failed to get disk usage")
		http.Error(w, "Failed to get disk usage", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(usage); err != nil {
		logrus.WithError(err).Error("Failed to encode disk usage response")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)

		return
	}
}
