package routes

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestRegisterUpdateRoute(t *testing.T) {
	app := testApp()
	auth := testAuthMiddleware()

	opts := config.Options{
		EnableUpdateAPI: true,
		UnblockHTTPAPI:  true,
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
	}

	registerUpdateRoute(context.Background(), app, auth, opts)

	routes := app.GetRoutes()
	found := false

	for _, r := range routes {
		if r.Path == "/v1/update" && r.Method == http.MethodPost {
			found = true

			break
		}
	}

	assert.True(t, found, "POST /v1/update should be registered")
}
