package auth

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/keyauth"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLogger returns a logger that writes to the test's log output.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// newNopLogger returns a logger that discards every event, for fuzz targets.
//
// Returns:
//   - *zerolog.Logger: Logger that discards all events.
func newNopLogger() *zerolog.Logger {
	return new(zerolog.Nop())
}

// authRequest sends a GET request through middleware and returns the status.
//
// Parameters:
//   - t: Test handle.
//   - middleware: Authentication middleware under test.
//   - authHeader: Authorization header value, or empty for none.
//   - cookie: Cookie header value, or empty for none.
//   - query: Raw query string, or empty for none.
//
// Returns:
//   - int: Response status code.
func authRequest(t *testing.T, middleware fiber.Handler, authHeader, cookie, query string) int {
	t.Helper()

	app := fiber.New(fiber.Config{})
	app.Get("/test", middleware, func(c fiber.Ctx) error {
		return c.SendString("authenticated")
	})

	path := "/test"
	if query != "" {
		path += "?" + query
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := app.Test(req)
	require.NoError(t, err)

	require.NoError(t, resp.Body.Close())

	return resp.StatusCode
}

func TestNewAPIAuthMiddleware(t *testing.T) {
	const testToken = "test-token-123"

	tests := []struct {
		name       string
		token      string
		authHeader string
		cookie     string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "empty token returns 401 with not configured message",
			token:      "",
			wantStatus: fiber.StatusUnauthorized,
			wantBody:   "API token not configured",
		},
		{
			name:       "valid bearer token returns 200",
			token:      testToken,
			authHeader: "Bearer " + testToken,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "raw authorization token returns 200",
			token:      testToken,
			authHeader: testToken,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "invalid bearer token returns 401",
			token:      testToken,
			authHeader: "Bearer wrong-token",
			wantStatus: fiber.StatusUnauthorized,
			wantBody:   keyauth.ErrMissingOrMalformedAPIKey.Error(),
		},
		{
			name:       "missing auth header returns 401",
			token:      testToken,
			wantStatus: fiber.StatusUnauthorized,
			wantBody:   keyauth.ErrMissingOrMalformedAPIKey.Error(),
		},
		{
			name:       "valid cookie fallback returns 200",
			token:      testToken,
			cookie:     "access_token=" + testToken,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "invalid cookie returns 401",
			token:      testToken,
			cookie:     "access_token=wrong-token",
			wantStatus: fiber.StatusUnauthorized,
			wantBody:   keyauth.ErrMissingOrMalformedAPIKey.Error(),
		},
		{
			name:       "non-bearer scheme falls through to raw value and fails",
			token:      testToken,
			authHeader: "NotBearer " + testToken,
			wantStatus: fiber.StatusUnauthorized,
			wantBody:   keyauth.ErrMissingOrMalformedAPIKey.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := NewAPIMiddleware(newTestLogger(t), tt.token)

			app := fiber.New(fiber.Config{})
			app.Get("/test", middleware, func(c fiber.Ctx) error {
				return c.SendString("authenticated")
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			if tt.cookie != "" {
				req.Header.Set("Cookie", tt.cookie)
			}

			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if tt.wantBody != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), tt.wantBody)
			}
		})
	}
}

func TestNewAPIAuthMiddleware_HashComputation(t *testing.T) {
	const testToken = "hash-test-token"

	middleware := NewAPIMiddleware(newTestLogger(t), testToken)

	app := fiber.New(fiber.Config{})
	app.Get("/test", middleware, func(c fiber.Ctx) error {
		return c.SendString("ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
}

// TestMiddlewareFallbackSources verifies that the API middleware falls back to
// the access_token cookie and the events middleware to the access_token query
// parameter, and that neither accepts the other's fallback.
func TestMiddlewareFallbackSources(t *testing.T) {
	t.Parallel()

	const testToken = "fallback-token"

	apiMiddleware := NewAPIMiddleware(newNopLogger(), testToken)
	eventsMiddleware := NewEventsMiddleware(newNopLogger(), testToken)

	tests := []struct {
		name       string
		middleware fiber.Handler
		authHeader string
		cookie     string
		query      string
		wantStatus int
	}{
		{name: "api bearer", middleware: apiMiddleware, authHeader: "Bearer " + testToken, wantStatus: fiber.StatusOK},
		{name: "api raw header", middleware: apiMiddleware, authHeader: testToken, wantStatus: fiber.StatusOK},
		{name: "api cookie", middleware: apiMiddleware, cookie: "access_token=" + testToken, wantStatus: fiber.StatusOK},
		{name: "api ignores query", middleware: apiMiddleware, query: "access_token=" + testToken, wantStatus: fiber.StatusUnauthorized},
		{name: "events bearer", middleware: eventsMiddleware, authHeader: "Bearer " + testToken, wantStatus: fiber.StatusOK},
		{name: "events raw header", middleware: eventsMiddleware, authHeader: testToken, wantStatus: fiber.StatusOK},
		{name: "events query", middleware: eventsMiddleware, query: "access_token=" + testToken, wantStatus: fiber.StatusOK},
		{name: "events ignores cookie", middleware: eventsMiddleware, cookie: "access_token=" + testToken, wantStatus: fiber.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.wantStatus, authRequest(t, tt.middleware, tt.authHeader, tt.cookie, tt.query))
		})
	}
}

// TestNewEventsMiddleware_NotConfigured verifies that an empty events token
// rejects every request with its own message.
func TestNewEventsMiddleware_NotConfigured(t *testing.T) {
	t.Parallel()

	app := fiber.New(fiber.Config{})
	app.Get("/test", NewEventsMiddleware(newNopLogger(), ""), func(c fiber.Ctx) error {
		return c.SendString("authenticated")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test?access_token=anything", nil)

	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, "events API token not configured", string(body))
}

// TestMiddlewareLogsFailedAttempts verifies that both middlewares log missing
// and invalid tokens at warn with notify=no, so the lines never reach
// notifications, and that a nil logger is accepted.
func TestMiddlewareLogsFailedAttempts(t *testing.T) {
	t.Parallel()

	constructors := map[string]func(*zerolog.Logger, string) fiber.Handler{
		"api":    NewAPIMiddleware,
		"events": NewEventsMiddleware,
	}

	for name, newAuth := range constructors {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			log := zerolog.New(&buf)
			middleware := newAuth(&log, "secret")

			assert.Equal(t, fiber.StatusUnauthorized, authRequest(t, middleware, "", "", ""))
			assert.Contains(t, buf.String(), `"message":"Missing or malformed API key"`)

			buf.Reset()

			assert.Equal(t, fiber.StatusUnauthorized, authRequest(t, middleware, "Bearer wrong", "", ""))
			assert.Contains(t, buf.String(), `"level":"warn"`)
			assert.Contains(t, buf.String(), `"notify":"no"`)
			assert.Contains(t, buf.String(), `"message":"Invalid token attempt"`)

			assert.Equal(t, fiber.StatusOK, authRequest(t, newAuth(nil, "secret"), "Bearer secret", "", ""))
			assert.Equal(t, fiber.StatusUnauthorized, authRequest(t, newAuth(nil, "secret"), "Bearer wrong", "", ""))
		})
	}
}

// TestExtractToken verifies the extraction order: the Bearer scheme, then the
// raw Authorization value, then the fallback source.
func TestExtractToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fallback   extractors.Extractor
		authHeader string
		query      string
		wantToken  string
		wantOK     bool
	}{
		{name: "missing", fallback: extractors.FromQuery(accessTokenKey), wantOK: false},
		{
			name:       "bearer",
			fallback:   extractors.FromQuery(accessTokenKey),
			authHeader: "Bearer events-secret",
			wantToken:  "events-secret",
			wantOK:     true,
		},
		{
			name:       "raw authorization swagger style",
			fallback:   extractors.FromQuery(accessTokenKey),
			authHeader: "events-secret",
			wantToken:  "events-secret",
			wantOK:     true,
		},
		{
			name:       "lowercase bearer prefix stripped from raw value",
			fallback:   extractors.FromQuery(accessTokenKey),
			authHeader: "bearer  events-secret",
			wantToken:  "events-secret",
			wantOK:     true,
		},
		{
			name:      "query fallback",
			fallback:  extractors.FromQuery(accessTokenKey),
			query:     "access_token=events-secret",
			wantToken: "events-secret",
			wantOK:    true,
		},
		{
			name:       "header wins over fallback",
			fallback:   extractors.FromQuery(accessTokenKey),
			authHeader: "Bearer header-secret",
			query:      "access_token=query-secret",
			wantToken:  "header-secret",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New()

			var got string

			var ok bool

			app.Get("/test", func(c fiber.Ctx) error {
				got, ok = extractToken(c, tt.fallback)

				return c.SendStatus(fiber.StatusOK)
			})

			path := "/test"
			if tt.query != "" {
				path += "?" + tt.query
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			resp, err := app.Test(req)
			require.NoError(t, err)

			require.NoError(t, resp.Body.Close())

			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantToken, got)
		})
	}
}
