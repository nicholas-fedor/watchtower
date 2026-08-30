package web

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

func TestDashboardRoutes(t *testing.T) {
	svc := control.New(store.NewMemory(), stream.NewMemory(), nil)
	run, err := svc.CreateRun(t.Context(), store.Spec{Generator: "file", Seed: 1}, "lab")
	require.NoError(t, err)
	require.NoError(t, svc.Store().UpsertCase(t.Context(), store.Case{
		RunID:   run.ID,
		CaseID:  "run-once_container_echo_deadbeef",
		Status:  store.CaseFail,
		Factors: map[string]string{"registry.persona": "lscr", "flag.debug": "unset"},
		Expect:  []byte(`{"outcome":"updated"}`),
		Argv:    []string{"watchtower", "--run-once"},
		Error:   "expected image id to change on update",
	}))

	app, _ := api.NewApp(svc, "")
	Mount(app, svc)

	t.Run("index", func(t *testing.T) {
		body := get(t, app, "/")
		require.Contains(t, body, "Watchtower e2e")
		require.Contains(t, body, run.ID[:8])
		require.Contains(t, body, `hx-history-elt`)
		require.Contains(t, body, `hx-boost:inherited="true"`)
		require.Contains(t, body, `hx-get="/"`)
		require.Contains(t, body, `hx-trigger="every 2s"`)
		require.Contains(t, body, `hx-swap="outerMorph"`)
		require.Contains(t, body, `hx-sync="this:replace"`)
		require.NotContains(t, body, `hx-trigger="load`)
		require.Contains(t, body, "<html")
	})
	t.Run("index partial", func(t *testing.T) {
		body := getHX(t, app, "/", "partial")
		require.Contains(t, body, `id="runs"`)
		require.NotContains(t, body, "<html")
	})
	t.Run("boosted index is full", func(t *testing.T) {
		body := getHX(t, app, "/", "full")
		require.Contains(t, body, "<html")
	})
	t.Run("run detail", func(t *testing.T) {
		body := get(t, app, "/runs/"+run.ID)
		require.Contains(t, body, "queued")
		require.Contains(t, body, "lab")
	})
	t.Run("run missing", func(t *testing.T) {
		body := get(t, app, "/runs/nope")
		require.Contains(t, body, "not found")
	})
	t.Run("static css", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/static/app.css", nil)
		require.NoError(t, err)
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Contains(t, resp.Header.Get("Content-Type"), "text/css")
		raw, _ := io.ReadAll(resp.Body)
		require.NotEmpty(t, raw)
	})
	t.Run("run poll keeps status", func(t *testing.T) {
		body := get(t, app, "/runs/"+run.ID+"?status=fail")
		require.Contains(t, body, `hx-get="/runs/`+run.ID+`?status=fail"`)
		require.Contains(t, body, `hx-push-url="true"`)
		require.Contains(t, body, `hx-trigger="every 2s"`)
		require.Contains(t, body, `hx-swap="outerMorph"`)
	})
	t.Run("run partial", func(t *testing.T) {
		body := getHX(t, app, "/runs/"+run.ID+"?status=fail", "partial")
		require.Contains(t, body, `id="run"`)
		require.NotContains(t, body, "<html")
	})
	t.Run("missing run does not poll", func(t *testing.T) {
		body := get(t, app, "/runs/nope")
		require.NotContains(t, body, `hx-trigger="every 2s"`)
	})
	t.Run("run table links to case", func(t *testing.T) {
		body := get(t, app, "/runs/"+run.ID)
		require.Contains(t, body, `/runs/`+run.ID+`/cases/run-once_container_echo_deadbeef`)
	})
	t.Run("case detail", func(t *testing.T) {
		body := get(t, app, "/runs/"+run.ID+"/cases/run-once_container_echo_deadbeef")
		require.Contains(t, body, "run-once_container_echo_deadbeef")
		require.Contains(t, body, "expected image id to change on update")
		require.Contains(t, body, "registry.persona")
		require.Contains(t, body, "lscr")
		require.NotContains(t, body, "flag.debug")
		require.Contains(t, body, "outcome")
		require.Contains(t, body, "updated")
		require.Contains(t, body, "watchtower")
		require.Contains(t, body, "What changed")
		require.Contains(t, body, "<html")
		require.NotContains(t, body, `hx-trigger="every 2s"`)
	})
	t.Run("case partial", func(t *testing.T) {
		body := getHX(t, app, "/runs/"+run.ID+"/cases/run-once_container_echo_deadbeef", "partial")
		require.Contains(t, body, `id="case"`)
		require.NotContains(t, body, "<html")
	})
	t.Run("case missing", func(t *testing.T) {
		body := get(t, app, "/runs/"+run.ID+"/cases/nope")
		require.Contains(t, body, "not found")
	})
}

func get(t *testing.T, app *fiber.App, path string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	require.NoError(t, err)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(raw), `"error":"unauthorized"`))

	return string(raw)
}

func getHX(t *testing.T, app *fiber.App, path, requestType string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	require.NoError(t, err)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-Request-Type", requestType)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return string(raw)
}
