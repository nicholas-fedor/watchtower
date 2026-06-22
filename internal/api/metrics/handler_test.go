package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	h := New()
	require.NotNil(t, h)
	assert.Equal(t, "/v1/metrics", h.Path)
	assert.NotNil(t, h.Handle)
}

func TestNew_ServesPrometheusMetrics(t *testing.T) {
	h := New()
	require.NotNil(t, h)

	app := fiber.New(fiber.Config{})
	app.Get("/v1/metrics", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/metrics", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify Prometheus format response
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	// Should contain Go runtime metrics (from default Prometheus registry)
	assert.NotEmpty(t, body, "response body should not be empty")
	// Prometheus text format uses # for comments and has metric names
	assert.Contains(t, body, "# HELP")
	assert.Contains(t, body, "# TYPE")
}

func TestNew_ReturnsTextContentType(t *testing.T) {
	h := New()
	require.NotNil(t, h)

	app := fiber.New(fiber.Config{})
	app.Get("/v1/metrics", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/metrics", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	assert.True(t, strings.Contains(contentType, "text/plain") || strings.Contains(contentType, "text/html"),
		"expected text content type, got: %s", contentType)
}
