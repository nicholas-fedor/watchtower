package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
)

func TestNew_ProxyConfig(t *testing.T) {
	app := New(
		logrus.New(),
		60,
		ProxyConfig{
			TrustedProxies: []string{"127.0.0.1"},
			ProxyHeader:    "X-Real-IP",
		},
		CORSConfig{},
		false,
	)
	assert.NotNil(t, app)

	app2 := New(
		logrus.New(),
		60,
		ProxyConfig{
			TrustedProxies: []string{"10.0.0.0/8"},
		},
		CORSConfig{},
		false,
	)
	assert.NotNil(t, app2)
}

func TestNew_CORSConfig(t *testing.T) {
	app := New(
		logrus.New(),
		60,
		ProxyConfig{},
		CORSConfig{
			AllowedOrigins: []string{"https://example.com"},
		},
		false,
	)
	assert.NotNil(t, app)

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodOptions,
		"/test",
		nil,
	)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	assert.Contains(t, allowOrigin, "https://example.com")
}

func TestNew_CORSWildcard(t *testing.T) {
	app := New(
		logrus.New(),
		60,
		ProxyConfig{},
		CORSConfig{
			AllowedOrigins: []string{"*"},
		},
		false,
	)
	assert.NotNil(t, app)

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodOptions,
		"/test",
		nil,
	)
	req.Header.Set("Origin", "https://any-origin.com")
	req.Header.Set("Access-Control-Request-Method", "POST")

	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	assert.Equal(t, "*", allowOrigin)

	allowMethods := resp.Header.Get("Access-Control-Allow-Methods")
	assert.Contains(t, allowMethods, "GET")
	assert.Contains(t, allowMethods, "POST")
}

func TestNew_CORSCustomMethodsHeaders(t *testing.T) {
	app := New(
		logrus.New(),
		60,
		ProxyConfig{},
		CORSConfig{
			AllowedOrigins: []string{"https://example.com"},
			AllowedMethods: []string{"GET", "POST"},
			AllowedHeaders: []string{"Content-Type", "X-Custom-Header"},
		},
		false,
	)
	assert.NotNil(t, app)

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodOptions,
		"/test",
		nil,
	)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "X-Custom-Header")

	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	allowMethods := resp.Header.Get("Access-Control-Allow-Methods")
	assert.Contains(t, allowMethods, "GET")
	assert.Contains(t, allowMethods, "POST")

	allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
	assert.Contains(t, allowHeaders, "X-Custom-Header")
}

func TestNew_NoCORSWhenNoOrigins(t *testing.T) {
	app := New(
		logrus.New(),
		60,
		ProxyConfig{},
		CORSConfig{},
		false,
	)
	assert.NotNil(t, app)

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/test",
		nil,
	)
	req.Header.Set("Origin", "https://example.com")

	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	assert.Empty(t, allowOrigin, "no CORS header should be set when no origins configured")
}

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
			got := New(
				tt.logrusLogger,
				tt.rateLimitPerMinute,
				ProxyConfig{},
				CORSConfig{},
				false,
			)
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
	handler := config.TimeoutMiddleware()
	require.NotNil(t, handler)

	app := fiber.New(fiber.Config{})
	app.Get("/test", handler, func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/test",
		nil,
	)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestTimeoutMiddleware_Timeout(t *testing.T) {
	const testTimeout = 10 * time.Millisecond

	handler := timeout.New(func(c fiber.Ctx) error {
		return c.Next()
	}, timeout.Config{
		Timeout: testTimeout,
	})
	require.NotNil(t, handler)

	app := fiber.New(fiber.Config{})
	app.Get("/test", handler, func(c fiber.Ctx) error {
		<-c.Context().Done()

		return fiber.ErrRequestTimeout
	})

	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"/test",
		nil,
	)

	resp, err := app.Test(req)
	require.NoError(t, err)

	synctest.Test(t, func(t *testing.T) {
		defer resp.Body.Close()

		assert.Equal(t, http.StatusRequestTimeout, resp.StatusCode)
	})
}

func TestNew_OnListenHook(t *testing.T) {
	var buf bytes.Buffer

	logger := logrus.New()
	logger.SetOutput(&buf)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logger.SetLevel(logrus.DebugLevel)

	app := New(logger, 60, ProxyConfig{}, CORSConfig{}, false)

	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	go func() {
		_ = app.Listen("127.0.0.1:0", fiber.ListenConfig{
			DisableStartupMessage: true,
		})
	}()

	require.Eventually(t, func() bool {
		return strings.Contains(buf.String(), "Starting HTTP API server")
	}, 2*time.Second, 50*time.Millisecond)

	output := buf.String()
	assert.Contains(t, output, "Starting HTTP API server")
	assert.Contains(t, output, "HTTP API server is enabled")
	assert.Contains(t, output, "host=")
	assert.Contains(t, output, "port=")
	assert.Contains(t, output, "tls=")

	_ = app.Shutdown()
}
