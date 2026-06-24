package events

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHandler(t *testing.T) {
	b := NewBroadcaster()
	h := NewHandler(b)
	require.NotNil(t, h)
	assert.Equal(t, "/v1/events", h.Path)
}

func TestHandler_Handle(t *testing.T) {
	b := NewBroadcaster()
	h := NewHandler(b)

	app := fiber.New(fiber.Config{})
	app.Get("/v1/events", h.Handle)

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/events", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestHandler_Handle_MaxSubscribers(t *testing.T) {
	b := NewBroadcaster()

	for range maxSubscribers {
		ch := b.Subscribe()
		assert.NotNil(t, ch, "subscriber channel should not be nil")
	}

	ch := b.Subscribe()
	assert.Nil(t, ch, "subscriber channel should be nil when max subscribers reached")
}

func TestHandler_Handle_NilBroadcaster(t *testing.T) {
	h := NewHandler(nil)
	app := fiber.New(fiber.Config{})
	app.Get("/v1/events", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/events", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestHandler_Handle_ReceivesEvent(t *testing.T) {
	b := NewBroadcaster()
	h := NewHandler(b)

	app := fiber.New(fiber.Config{})
	app.Get("/v1/events", h.Handle)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/events", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
}
