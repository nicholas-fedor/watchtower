package routes

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	mockRoutes "github.com/nicholas-fedor/watchtower/internal/api/routes/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/observability/metrics"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/schedule"
)

func TestRegisterCheckRoute(t *testing.T) {
	t.Run("nil client does not register route", func(t *testing.T) {
		app := testApp()
		auth := testAuthMiddleware()

		opts := Options{
			EnableCheckAPI: true,
			Client:         nil,
		}

		registerCheckRoute(app, auth, opts)

		routes := app.GetRoutes()
		for _, r := range routes {
			if r.Path == "/v1/check" {
				t.Error("POST /v1/check should not be registered when client is nil")
			}
		}
	})

	t.Run("with client registers route", func(t *testing.T) {
		app := testApp()
		auth := testAuthMiddleware()
		mockClient := mockRoutes.NewMockClient(t)

		opts := Options{
			EnableCheckAPI:   true,
			Client:           mockClient,
			FilterByImage:    func(_ []string, f filters.Filter) filters.Filter { return f },
			EventBroadcaster: events.NewBroadcaster(),
		}

		registerCheckRoute(app, auth, opts)

		routes := app.GetRoutes()
		found := false

		for _, r := range routes {
			if r.Path == "/v1/check" && r.Method == http.MethodPost {
				found = true

				break
			}
		}

		assert.True(t, found, "POST /v1/check should be registered")
	})
}

func TestRegisterEventsRoute(t *testing.T) {
	app := testApp()

	opts := Options{
		EnableEventsAPI:    true,
		EventsToken:        "events-token",
		CORSAllowedOrigins: []string{"https://example.com"},
		EventBroadcaster:   events.NewBroadcaster(),
	}

	registerEventsRoute(app, opts)

	routes := app.GetRoutes()
	found := false

	for _, r := range routes {
		if r.Path == "/v1/events" && r.Method == http.MethodGet {
			found = true

			break
		}
	}

	assert.True(t, found, "GET /v1/events should be registered")
}

func TestRegisterUpdateRoute_WriteStartupMessage(t *testing.T) {
	app := testApp()
	auth := testAuthMiddleware()

	opts := Options{
		EnableUpdateAPI: true,
		UpdateLock:      schedule.NewUpdateLock(),
		UnblockHTTPAPI:  false,
		RunUpdatesWithNotifications: func(_ context.Context, _ filters.Filter, _ updateParams.Params) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f filters.Filter) filters.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
		WriteStartupMessage: func(context.Context, logging.StartupParams) {
		},
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
