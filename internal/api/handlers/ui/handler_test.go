package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "correct-token"

// newTestApp mounts the UI handler the same way the routes package does.
func newTestApp(t *testing.T, opts Options) *fiber.App {
	t.Helper()

	handler, err := New(nil, opts)
	require.NoError(t, err)

	app := fiber.New(fiber.Config{StrictRouting: true, CaseSensitive: true})
	app.Get("/", handler.Redirect)
	app.Get("/ui", handler.Redirect)
	app.Get(BasePath, handler.Index)
	app.Get(AssetsPath, handler.Asset)
	app.Get(SessionPath, handler.Session)
	app.Post(SessionPath, handler.SignIn)
	app.Delete(SessionPath, handler.SignOut)

	return app
}

// result is a response with its body already read and closed.
type result struct {
	status  int
	header  http.Header
	cookies []*http.Cookie
}

// do sends a request and returns the response details and body.
func do(t *testing.T, app *fiber.App, req *http.Request) (result, string) {
	t.Helper()

	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return result{status: resp.StatusCode, header: resp.Header, cookies: resp.Cookies()}, string(body)
}

// sessionCookie returns the session cookie set by a response, if any.
func sessionCookie(resp result) *http.Cookie {
	for _, cookie := range resp.cookies {
		if cookie.Name == CookieName {
			return cookie
		}
	}

	return nil
}

func signIn(t *testing.T, app *fiber.App, body string) (result, string) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, SessionPath, strings.NewReader(body))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	return do(t, app, req)
}

func TestRedirect(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, Options{Token: testToken})

	for _, target := range []string{"/", "/ui"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		resp, _ := do(t, app, req)

		assert.Equal(t, http.StatusFound, resp.status, target)
		assert.Equal(t, BasePath, resp.header.Get(fiber.HeaderLocation), target)
	}
}

func TestIndex(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, Options{Token: testToken})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, BasePath, nil)
	resp, body := do(t, app, req)

	assert.Equal(t, http.StatusOK, resp.status)
	assert.Equal(t, "text/html; charset=utf-8", resp.header.Get(fiber.HeaderContentType))
	assert.Equal(t, contentSecurityPolicy, resp.header.Get(fiber.HeaderContentSecurityPolicy))
	assert.Equal(t, cacheRevalidate, resp.header.Get(fiber.HeaderCacheControl))
	assert.NotEmpty(t, resp.header.Get(fiber.HeaderETag))
	assert.Contains(t, body, `<div id="app">`)
	assert.NotContains(t, body, testToken)
}

func TestIndexReferencesEmbeddedAssets(t *testing.T) {
	t.Parallel()

	handler, err := New(nil, Options{Token: testToken})
	require.NoError(t, err)

	index := string(handler.assets[indexFile].body)

	for _, name := range []string{"app.css", "app.js", "favicon.svg", "fonts/barlow-condensed-600.woff2"} {
		assert.Contains(t, index, "assets/"+name)
		assert.Contains(t, handler.assets, name)
	}
}

func TestAsset(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, Options{Token: testToken})

	tests := []struct {
		name         string
		contentType  string
		cacheControl string
	}{
		{name: "app.css", contentType: "text/css; charset=utf-8", cacheControl: cacheRevalidate},
		{name: "app.js", contentType: "text/javascript; charset=utf-8", cacheControl: cacheRevalidate},
		{name: "favicon.svg", contentType: "image/svg+xml", cacheControl: cacheRevalidate},
		{name: "fonts/barlow-400.woff2", contentType: "font/woff2", cacheControl: cacheFonts},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ui/assets/"+tt.name, nil)
			resp, body := do(t, app, req)

			assert.Equal(t, http.StatusOK, resp.status)
			assert.Equal(t, tt.contentType, resp.header.Get(fiber.HeaderContentType))
			assert.Equal(t, tt.cacheControl, resp.header.Get(fiber.HeaderCacheControl))
			assert.NotEmpty(t, body)
		})
	}
}

func TestAssetNotFound(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, Options{Token: testToken})

	for _, name := range []string{"missing.js", "../handler.go", "fonts/"} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ui/assets/"+name, nil)
		resp, _ := do(t, app, req)

		assert.Equal(t, http.StatusNotFound, resp.status, name)
	}
}

func TestAssetNotModified(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, Options{Token: testToken})

	first := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ui/assets/app.css", nil)
	resp, _ := do(t, app, first)
	etag := resp.header.Get(fiber.HeaderETag)
	require.NotEmpty(t, etag)

	second := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/ui/assets/app.css", nil)
	second.Header.Set(fiber.HeaderIfNoneMatch, etag)

	resp, body := do(t, app, second)

	assert.Equal(t, http.StatusNotModified, resp.status)
	assert.Empty(t, body)
}

func TestSessionSignedOut(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, Options{Token: testToken, Version: "v1.2.3", Features: Features{Update: true}})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, SessionPath, nil)
	resp, body := do(t, app, req)

	assert.Equal(t, http.StatusOK, resp.status)
	assert.Equal(t, "no-store", resp.header.Get(fiber.HeaderCacheControl))
	assert.JSONEq(t, `{"authenticated":false}`, body)
}

func TestSessionRejectsWrongCookie(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, Options{Token: testToken})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, SessionPath, nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "wrong-token"})

	_, body := do(t, app, req)

	assert.JSONEq(t, `{"authenticated":false}`, body)
}

func TestSignIn(t *testing.T) {
	t.Parallel()

	features := Features{Containers: true, Check: true, History: true}
	app := newTestApp(t, Options{Token: testToken, Version: "v1.2.3", Features: features})

	resp, body := signIn(t, app, `{"token":"`+testToken+`"}`)
	require.Equal(t, http.StatusOK, resp.status)

	var session struct {
		Authenticated bool     `json:"authenticated"`
		Version       string   `json:"version"`
		Features      Features `json:"features"`
	}

	require.NoError(t, json.Unmarshal([]byte(body), &session))
	assert.True(t, session.Authenticated)
	assert.Equal(t, "v1.2.3", session.Version)
	assert.Equal(t, features, session.Features)

	cookie := sessionCookie(resp)
	require.NotNil(t, cookie)
	assert.Equal(t, testToken, cookie.Value)
	assert.Equal(t, "/", cookie.Path)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	assert.Positive(t, cookie.MaxAge)

	// The cookie signs later requests in.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, SessionPath, nil)
	req.AddCookie(cookie)

	_, body = do(t, app, req)
	require.NoError(t, json.Unmarshal([]byte(body), &session))
	assert.True(t, session.Authenticated)
}

func TestSignInRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		body  string
	}{
		{name: "wrong token", token: testToken, body: `{"token":"wrong-token"}`},
		{name: "empty token", token: testToken, body: `{"token":""}`},
		{name: "missing field", token: testToken, body: `{}`},
		{name: "not JSON", token: testToken, body: `token=` + testToken},
		{name: "empty body", token: testToken, body: ``},
		{name: "no token configured", token: "", body: `{"token":""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := newTestApp(t, Options{Token: tt.token, Version: "v1.2.3"})

			resp, body := signIn(t, app, tt.body)

			assert.Equal(t, http.StatusUnauthorized, resp.status)
			assert.Nil(t, sessionCookie(resp))
			assert.NotContains(t, body, "v1.2.3")
		})
	}
}

func TestSignOut(t *testing.T) {
	t.Parallel()

	app := newTestApp(t, Options{Token: testToken})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, SessionPath, nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: testToken})

	resp, body := do(t, app, req)

	assert.Equal(t, http.StatusOK, resp.status)
	assert.JSONEq(t, `{"authenticated":false}`, body)

	cookie := sessionCookie(resp)
	require.NotNil(t, cookie)
	assert.Empty(t, cookie.Value)
	assert.Negative(t, cookie.MaxAge)
}
