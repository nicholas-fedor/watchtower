package settings

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

	runtime "github.com/nicholas-fedor/watchtower/internal/settings"
)

func newTestApp(t *testing.T, scheduleActive bool) (*fiber.App, *runtime.Store) {
	t.Helper()

	store := runtime.Open(nil, t.TempDir(), runtime.Values{Cleanup: true, Schedule: "@every 24h"}, runtime.Validators{})
	handler := New(nil, store, scheduleActive)

	app := fiber.New()
	app.Get(handler.Path, handler.Get)
	app.Patch(handler.Path, handler.Patch)

	return app, store
}

func send(t *testing.T, app *fiber.App, method, body string) (int, string) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, "/v1/settings", strings.NewReader(body))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, string(data)
}

func TestNewPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/v1/settings", New(nil, nil, true).Path)
}

func TestGet(t *testing.T) {
	t.Parallel()

	app, _ := newTestApp(t, true)

	status, body := send(t, app, http.MethodGet, "")
	require.Equal(t, http.StatusOK, status)

	var response Response
	require.NoError(t, json.Unmarshal([]byte(body), &response))

	assert.True(t, response.Saved)
	assert.True(t, response.ScheduleActive)
	assert.Equal(t, "v1", response.APIVersion)
	assert.Equal(t, Setting{Value: true, Default: true, Changed: false}, response.Settings[runtime.KeyCleanup])
	assert.Equal(t, Setting{Value: false, Default: false, Changed: false}, response.Settings[runtime.KeyMonitorOnly])
	assert.Equal(t,
		Setting{Value: "@every 24h", Default: "@every 24h", Changed: false},
		response.Settings[runtime.KeySchedule],
	)
	assert.Contains(t, response.Settings, runtime.KeyRollingRestart)
}

func TestPatch(t *testing.T) {
	t.Parallel()

	app, store := newTestApp(t, false)

	status, body := send(t, app, http.MethodPatch, `{"monitor_only":true,"schedule":"@every 6h"}`)
	require.Equal(t, http.StatusOK, status)

	var response Response
	require.NoError(t, json.Unmarshal([]byte(body), &response))

	assert.False(t, response.ScheduleActive)
	assert.Equal(t, Setting{Value: true, Default: false, Changed: true}, response.Settings[runtime.KeyMonitorOnly])
	assert.Equal(t,
		Setting{Value: "@every 6h", Default: "@every 24h", Changed: true},
		response.Settings[runtime.KeySchedule],
	)
	assert.True(t, store.Effective().MonitorOnly)

	// JSON null returns a setting to its default.
	status, body = send(t, app, http.MethodPatch, `{"monitor_only":null}`)
	require.Equal(t, http.StatusOK, status)
	require.NoError(t, json.Unmarshal([]byte(body), &response))
	assert.Equal(t, Setting{Value: false, Default: false, Changed: false}, response.Settings[runtime.KeyMonitorOnly])
}

func TestPatchRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "empty body", body: ``, want: "JSON object"},
		{name: "not an object", body: `[true]`, want: "JSON object"},
		{name: "no settings", body: `{}`, want: "JSON object"},
		{name: "unknown setting", body: `{"no_pull":true}`, want: "unknown setting"},
		{name: "wrong type", body: `{"cleanup":"yes"}`, want: "invalid value"},
		{name: "incompatible", body: `{"monitor_only":true,"rolling_restart":true}`, want: "rolling restart"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app, store := newTestApp(t, true)
			before := store.Effective()

			status, body := send(t, app, http.MethodPatch, tt.body)

			assert.Equal(t, http.StatusBadRequest, status)

			var response map[string]string
			require.NoError(t, json.Unmarshal([]byte(body), &response))
			assert.Contains(t, response["error"], tt.want)
			assert.Equal(t, before, store.Effective())
		})
	}
}

func TestTestNotificationNoURLs(t *testing.T) {
	t.Parallel()

	app, _ := newTestApp(t, true)
	handler := NewWithNotifier(nil, nil, true, nil)
	app.Post("/v1/settings/test-notification", handler.TestNotification)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/settings/test-notification", strings.NewReader(`{}`))
	req.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)

	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

