package registry

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHubChallengeAndToken(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{}`))
	}))
	t.Cleanup(backend.Close)

	proxy, err := NewProxy(PersonaHub, backend.URL, NewController())
	require.NoError(t, err)

	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)

	challenge := doGet(t, server.URL+"/v2/")
	t.Cleanup(func() { _ = challenge.Body.Close() })
	require.Equal(t, http.StatusUnauthorized, challenge.StatusCode)
	assert.Contains(t, challenge.Header.Get("WWW-Authenticate"), "auth.docker.io/token")
	assert.Contains(t, challenge.Header.Get("WWW-Authenticate"), "realm=")
	assert.Contains(t, challenge.Header.Get("WWW-Authenticate"), "service=")
	assert.Contains(t, challenge.Header.Get("WWW-Authenticate"), "scope=")

	tokenResp := doGet(t, server.URL+"/token")
	t.Cleanup(func() { _ = tokenResp.Body.Close() })
	require.Equal(t, http.StatusOK, tokenResp.StatusCode)

	raw, readErr := io.ReadAll(tokenResp.Body)
	require.NoError(t, readErr)

	var payload map[string]string

	require.NoError(t, json.Unmarshal(raw, &payload))
	require.Equal(t, issuedAccess, payload["token"])

	authed := doGetAuth(t, server.URL+"/v2/", issuedAccess)
	t.Cleanup(func() { _ = authed.Body.Close() })
	assert.Equal(t, http.StatusOK, authed.StatusCode)
}

func TestGHCR429Body(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	control := NewController()
	control.SetFault(FaultGHCR429, 0)

	proxy, err := NewProxy(PersonaGHCR, backend.URL, control)
	require.NoError(t, err)

	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)

	resp := doGet(t, server.URL+"/v2/e2e/app/manifests/latest")
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)

	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	assert.Equal(t, GHCRTooManyRequests, string(body))
	assert.Contains(t, string(body), "retry-after:")
	assert.Contains(t, string(body), "allowed: 44000/minute")
}

func TestHub429Headers(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	control := NewController()
	control.SetFault(FaultHub429, 0)

	proxy, err := NewProxy(PersonaHub, backend.URL, control)
	require.NoError(t, err)

	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)

	resp := doGet(t, server.URL+"/v2/e2e/app/manifests/latest")
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, HubRetryAfterSeconds, resp.Header.Get("Retry-After"))
	assert.Equal(t, HubRateLimitLimit, resp.Header.Get("Ratelimit-Limit"))
	assert.Equal(t, HubRateLimitRemaining, resp.Header.Get("Ratelimit-Remaining"))
}

func TestLSCRNameServed(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v2/e2e/app/manifests/r1" {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("manifest"))

			return
		}

		writer.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(backend.Close)

	proxy, err := NewProxy(PersonaLSCR, backend.URL, NewController())
	require.NoError(t, err)

	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)

	hosts := HostsFor(PersonaLSCR)
	require.Contains(t, hosts, "lscr.io")
	require.Contains(t, hosts, "ghcr.io")

	req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/v2/e2e/app/manifests/r1", nil)
	require.NoError(t, reqErr)

	req.Host = "lscr.io"

	resp, doErr := http.DefaultClient.Do(req)
	require.NoError(t, doErr)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	assert.Equal(t, "manifest", string(body))
}

func TestControlFaultTripAfterN(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	t.Cleanup(backend.Close)

	control := NewController()
	proxy, err := NewProxy(PersonaHub, backend.URL, control)
	require.NoError(t, err)

	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)

	body := strings.NewReader(`{"fault":"429-hub","after":1}`)
	req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/e2e-control/fault", body)
	require.NoError(t, reqErr)
	req.Header.Set("Content-Type", "application/json")

	ctl, doErr := http.DefaultClient.Do(req)
	require.NoError(t, doErr)
	t.Cleanup(func() { _ = ctl.Body.Close() })
	require.Equal(t, http.StatusNoContent, ctl.StatusCode)

	first := doGet(t, server.URL+"/v2/e2e/app/manifests/latest")
	t.Cleanup(func() { _ = first.Body.Close() })
	require.Equal(t, http.StatusOK, first.StatusCode)

	second := doGet(t, server.URL+"/v2/e2e/app/manifests/latest")
	t.Cleanup(func() { _ = second.Body.Close() })
	require.Equal(t, http.StatusTooManyRequests, second.StatusCode)
}

// doGet issues GET rawURL with t.Context and fails the test on error.
//
// Parameters:
//   - t: Test handle.
//   - rawURL: Request URL.
//
// Returns:
//   - *http.Response: Response. Caller must close Body.
func doGet(t *testing.T, rawURL string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	require.NoError(t, err)

	resp, doErr := http.DefaultClient.Do(req)
	require.NoError(t, doErr)

	return resp
}

// doGetAuth issues GET rawURL with a Bearer token.
//
// Parameters:
//   - t: Test handle.
//   - rawURL: Request URL.
//   - bearer: Token value without the Bearer prefix.
//
// Returns:
//   - *http.Response: Response. Caller must close Body.
func doGetAuth(t *testing.T, rawURL, bearer string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, doErr := http.DefaultClient.Do(req)
	require.NoError(t, doErr)

	return resp
}
