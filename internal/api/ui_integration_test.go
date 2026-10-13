package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/api/handlers/ui"
)

// TestWebUISignInAuthorizesAPI verifies that the cookie set by web UI sign-in
// is accepted by the auth middleware on protected /v1/* routes.
func TestWebUISignInAuthorizesAPI(t *testing.T) {
	app := newFullSurfaceApp(t)

	// Without a session the protected route rejects the request.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/config", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	signIn := httptest.NewRequestWithContext(
		t.Context(), http.MethodPost, ui.SessionPath, strings.NewReader(`{"token":"api-token"}`),
	)
	signIn.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	resp, err = app.Test(signIn)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var session *http.Cookie

	for _, cookie := range resp.Cookies() {
		if cookie.Name == ui.CookieName {
			session = cookie
		}
	}

	require.NotNil(t, session, "sign-in must set the session cookie")

	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/config", nil)
	req.AddCookie(session)

	resp, err = app.Test(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
