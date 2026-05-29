// Package containers provides the /v1/containers HTTP API endpoint, exposing the
// current image identity (name, local ID, and registry manifest digest) of each
// container Watchtower watches.
//
// This lets an external orchestrator compare what each container is actually
// running against a registry's current digest without pulling any image layers.
package containers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// Status describes a single watched container's current image identity.
type Status struct {
	// Name is the container name.
	Name string `json:"name"`
	// Image is the image reference with tag (e.g. ethpandaops/lighthouse:latest).
	Image string `json:"image"`
	// ImageID is the local image config ID (sha256:...).
	ImageID string `json:"image_id"`
	// RunningDigest is the registry manifest digest the running image was pulled
	// from (sha256:...), derived from the image's RepoDigests. It is directly
	// comparable to a registry's Docker-Content-Digest. Empty for locally-built
	// images with no registry reference.
	RunningDigest string `json:"running_digest"`
}

// ListFunc returns the current status of all watched containers.
type ListFunc func(ctx context.Context) ([]Status, error)

// Handler serves the /v1/containers endpoint.
//
// It holds the list function and endpoint path for the read-only
// /v1/containers endpoint.
type Handler struct {
	list ListFunc // Container status lookup function.
	Path string   // API endpoint path (e.g., "/v1/containers").
}

// New creates a new containers Handler backed by the given list function.
//
// Parameters:
//   - list: Function returning the current status of all watched containers.
//
// Returns:
//   - *Handler: Initialized handler serving /v1/containers.
func New(list ListFunc) *Handler {
	return &Handler{
		list: list,
		Path: "/v1/containers",
	}
}

// Handle responds with the JSON status of every watched container.
//
// Parameters:
//   - w: HTTP response writer for sending the JSON payload or error status.
//   - r: HTTP request; its context is propagated to the Docker calls.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	logrus.WithFields(logrus.Fields{
		"method": r.Method,
		"path":   r.URL.Path,
	}).Debug("Received HTTP API containers request")

	statuses, err := h.list(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to list containers for API")
		http.Error(w, "failed to list containers", http.StatusInternalServerError)

		return
	}

	response := map[string]any{
		"containers":  statuses,
		"count":       len(statuses),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"api_version": "v1",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logrus.WithError(err).Error("Failed to encode containers response")
	}
}
