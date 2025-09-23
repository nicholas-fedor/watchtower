package metrics

import (
	"encoding/json"
	"net/http"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

// Handler is an HTTP handle for serving metric data.
type Handler struct {
	Path    string
	Handle  http.HandlerFunc
	Metrics *metrics.Metrics
}

// New is a factory function creating a new Metrics instance.
func New() *Handler {
	metrics := metrics.Default()
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		data := map[string]any{
			"scanned": metrics.GetScanned(),
			"updated": metrics.GetUpdated(),
			"failed":  metrics.GetFailed(),
		}
		json.NewEncoder(w).Encode(data)
	}

	return &Handler{
		Path:    "/v1/metrics",
		Handle:  handler,
		Metrics: metrics,
	}
}
