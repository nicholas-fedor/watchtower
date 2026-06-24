package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name               string
		logrusLogger       *logrus.Logger
		rateLimitPerMinute int
		wantNil            bool
	}{
		{
			name:               "default rate limit",
			logrusLogger:       logrus.New(),
			rateLimitPerMinute: 60,
			wantNil:            false,
		},
		{
			name:               "zero rate limit falls back to default",
			logrusLogger:       logrus.New(),
			rateLimitPerMinute: 0,
			wantNil:            false,
		},
		{
			name:               "negative rate limit falls back to default",
			logrusLogger:       logrus.New(),
			rateLimitPerMinute: -1,
			wantNil:            false,
		},
		{
			name:               "custom rate limit",
			logrusLogger:       logrus.New(),
			rateLimitPerMinute: 120,
			wantNil:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New(tt.logrusLogger, tt.rateLimitPerMinute, ProxyConfig{}, CORSConfig{})
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.IsType(t, &fiber.App{}, got)
			}
		})
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	handler := TimeoutMiddleware()
	require.NotNil(t, handler)

	app := fiber.New(fiber.Config{})
	app.Get("/test", handler, func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestTimeoutMiddleware_Timeout(t *testing.T) {
	handler := TimeoutMiddleware()
	require.NotNil(t, handler)

	app := fiber.New(fiber.Config{})
	app.Get("/test", handler, func(c fiber.Ctx) error {
		time.Sleep(2 * time.Hour)

		return c.SendString("should not reach")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)

	resp, err := app.Test(req)
	if err != nil {
		// Expected: context deadline exceeded from timeout
		assert.Contains(t, err.Error(), "timeout")

		return
	}

	defer resp.Body.Close()

	assert.Equal(t, http.StatusRequestTimeout, resp.StatusCode)
}
