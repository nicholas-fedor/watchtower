package routes

import (
	"context"
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/check"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/events"
	"github.com/nicholas-fedor/watchtower/internal/logging"
	"github.com/nicholas-fedor/watchtower/internal/metrics"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestRegisterCheckRoute(t *testing.T) {
	t.Run("nil client does not register route", func(t *testing.T) {
		app := testApp()
		auth := testAuthMiddleware()

		opts := config.Options{
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
		mockClient := mockContainer.NewMockClient(t)

		opts := config.Options{
			EnableCheckAPI:   true,
			Client:           mockClient,
			FilterByImage:    func(_ []string, f types.Filter) types.Filter { return f },
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

	opts := config.Options{
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

	opts := config.Options{
		EnableUpdateAPI: true,
		UnblockHTTPAPI:  false,
		RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
			return &metrics.Metric{}
		},
		FilterByImage:  func(_ []string, f types.Filter) types.Filter { return f },
		DefaultMetrics: func() *metrics.Metrics { return testMetrics },
		WriteStartupMessage: func(_ logging.StartupParams) {
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

func TestRecordCheck(t *testing.T) {
	registry := prometheus.NewRegistry()

	store, err := metrics.NewWithRegistry(registry)
	require.NoError(t, err)

	t.Cleanup(store.Shutdown)

	opts := config.Options{DefaultMetrics: func() *metrics.Metrics { return store }}

	recordCheck(opts, []check.ContainerCheck{
		{Name: "current"},
		{Name: "waiting", UpdateAvailable: true},
		{Name: "also-waiting", UpdateAvailable: true},
		{Name: "unreachable", UpdateAvailable: true, Error: "registry returned 429"},
	})

	history := store.GetHistory(nil, nil, 0)
	require.Len(t, history, 1)
	assert.Equal(t, metrics.HistoryKindCheck, history[0].Kind)
	assert.Equal(t, 4, history[0].Scanned)
	assert.Equal(t, 2, history[0].UpdatesAvailable)
	assert.Equal(t, 1, history[0].Failed)
}

func TestRecordCheck_NoMetricsStore(t *testing.T) {
	assert.NotPanics(t, func() {
		recordCheck(config.Options{}, []check.ContainerCheck{{Name: "current"}})
		recordCheck(
			config.Options{DefaultMetrics: func() *metrics.Metrics { return nil }},
			[]check.ContainerCheck{{Name: "current"}},
		)
	})
}
