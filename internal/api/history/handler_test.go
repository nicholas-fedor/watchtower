package history

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

func TestNew(t *testing.T) {
	h := New(func(_, _ *time.Time, _ int) []metrics.HistoryEntry { return nil })
	require.NotNil(t, h)
	assert.Equal(t, "/v1/history", h.Path)
}

func TestHandler_Handle(t *testing.T) {
	entries := []metrics.HistoryEntry{
		{Scanned: 5, Updated: 2, Failed: 0, Restarted: 0, Skipped: 3},
		{Scanned: 5, Updated: 1, Failed: 0, Restarted: 0, Skipped: 4},
	}

	h := New(func(_, _ *time.Time, _ int) []metrics.HistoryEntry { return entries })

	app := fiber.New(fiber.Config{})
	app.Get("/v1/history", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/history", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandler_HandleWithInvalidSince(t *testing.T) {
	h := New(func(_, _ *time.Time, _ int) []metrics.HistoryEntry { return nil })

	app := fiber.New(fiber.Config{})
	app.Get("/v1/history", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/history?since=not-a-date", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandler_HandleWithInvalidLimit(t *testing.T) {
	h := New(func(_, _ *time.Time, _ int) []metrics.HistoryEntry { return nil })

	app := fiber.New(fiber.Config{})
	app.Get("/v1/history", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/history?limit=abc", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
