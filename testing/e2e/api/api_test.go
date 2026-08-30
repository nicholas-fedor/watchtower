package api

import (
	"io"
	"net/http"
	"strings"
	"testing"

	json "encoding/json/v2"

	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
	"github.com/nicholas-fedor/watchtower/testing/e2e/web"
)

func TestHealthAndCreateRun(t *testing.T) {
	svc := control.New(store.NewMemory(), stream.NewMemory(), nil)
	app, _ := NewApp(svc, "")

	resp, err := app.Test(newRequest(t, http.MethodGet, "/v1/health", nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	body := `{"generator":"product","topic":"cleanup","limit":20,"seed":1}`
	resp, err = app.Test(newRequest(t, http.MethodPost, "/v1/runs", strings.NewReader(body)))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var run store.Run
	require.NoError(t, json.UnmarshalRead(resp.Body, &run))
	require.NoError(t, resp.Body.Close())
	require.NotEmpty(t, run.ID)
	require.Equal(t, store.RunQueued, run.Status)
	require.Equal(t, "cleanup", run.Spec.Topic)

	resp, err = app.Test(newRequest(t, http.MethodGet, "/v1/runs/"+run.ID, nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	resp, err = app.Test(newRequest(t, http.MethodGet, "/v1/runs/does-not-exist", nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}

func TestAuthBearer(t *testing.T) {
	svc := control.New(store.NewMemory(), stream.NewMemory(), nil)
	app, _ := NewApp(svc, "secret")

	resp, err := app.Test(newRequest(t, http.MethodGet, "/v1/runs", nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	req := newRequest(t, http.MethodGet, "/v1/health", nil)
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	req = newRequest(t, http.MethodGet, "/v1/runs", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}

func TestAuthDashboardPublicAPIProtected(t *testing.T) {
	svc := control.New(store.NewMemory(), stream.NewMemory(), nil)
	run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file", Seed: 1}, "lab")
	require.NoError(t, err)

	app, _ := NewApp(svc, "secret")
	web.Mount(app, svc)

	tests := []struct {
		name   string
		path   string
		status int
		auth   string
	}{
		{name: "health", path: "/v1/health", status: http.StatusOK},
		{name: "index", path: "/", status: http.StatusOK},
		{name: "static", path: "/static/app.css", status: http.StatusOK},
		{name: "run page", path: "/runs/" + run.ID, status: http.StatusOK},
		{name: "api list", path: "/v1/runs", status: http.StatusUnauthorized},
		{name: "api get", path: "/v1/runs/" + run.ID, status: http.StatusUnauthorized},
		{name: "bearer", path: "/v1/runs", status: http.StatusOK, auth: "Bearer secret"},
		{name: "raw token", path: "/v1/runs", status: http.StatusUnauthorized, auth: "secret"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequest(t, http.MethodGet, tc.path, nil)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}

			resp, getErr := app.Test(req)
			require.NoError(t, getErr)
			require.Equal(t, tc.status, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
		})
	}
}

func newRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, target, body)
	require.NoError(t, err)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req
}
