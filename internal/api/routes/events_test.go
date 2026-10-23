package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/observability/events"
)

func TestRegisterEventsRoute_RejectsMissingToken(t *testing.T) {
	app := testApp()
	opts := Options{
		EnableEventsAPI:  true,
		EventsToken:      "events-secret",
		EventBroadcaster: events.NewBroadcaster(),
	}
	registerEventsRoute(app, opts)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/events", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestRegisterEventsRoute_HashCompareRejectsWrongLength(t *testing.T) {
	app := testApp()
	opts := Options{
		EnableEventsAPI:  true,
		EventsToken:      "events-secret",
		EventBroadcaster: events.NewBroadcaster(),
	}
	registerEventsRoute(app, opts)

	// Different length must not panic and must not authenticate.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/events", nil)
	req.Header.Set("Authorization", "x")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestRegisterEventsRoute_RejectsWrongToken(t *testing.T) {
	app := testApp()
	opts := Options{
		EnableEventsAPI:  true,
		EventsToken:      "events-secret",
		EventBroadcaster: events.NewBroadcaster(),
	}
	registerEventsRoute(app, opts)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/events", nil)
	req.Header.Set("Authorization", "wrong")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	_ = resp.Body.Close()
}
