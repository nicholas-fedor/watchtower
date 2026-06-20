package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

func TestNew(t *testing.T) {
	updateFn := func(_ context.Context, _, _ []string) *metrics.Metric {
		return &metrics.Metric{}
	}

	tests := []struct {
		name       string
		updateFn   func(ctx context.Context, images, containers []string) *metrics.Metric
		updateLock chan bool
		wantPath   string
	}{
		{
			name:       "with nil lock creates new lock",
			updateFn:   updateFn,
			updateLock: nil,
			wantPath:   "/v1/update",
		},
		{
			name:       "with provided lock uses it",
			updateFn:   updateFn,
			updateLock: make(chan bool, 1),
			wantPath:   "/v1/update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.updateFn, tt.updateLock)
			require.NotNil(t, h)
			assert.Equal(t, tt.wantPath, h.Path)
		})
	}
}

func TestHandler_Handle_Sync(t *testing.T) {
	var called atomic.Int32

	expectedMetric := &metrics.Metric{Scanned: 5, Updated: 2, Failed: 1, Restarted: 1, Skipped: 0}

	updateFn := func(_ context.Context, _, _ []string) *metrics.Metric {
		called.Add(1)

		return expectedMetric
	}

	lock := make(chan bool, 1)
	lock <- true

	h := New(updateFn, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/update", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(1), called.Load())
}

func TestHandler_Handle_Async(t *testing.T) {
	var called atomic.Int32

	lock := make(chan bool, 1)
	lock <- true

	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		called.Add(1)

		return &metrics.Metric{}
	}, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/update?async=true", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), called.Load())
}

func TestHandler_Handle_FullUpdateLocked(t *testing.T) {
	lock := make(chan bool, 1)
	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		t.Error("update function should not be called when lock is held")

		return &metrics.Metric{}
	}, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/update", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "30", resp.Header.Get("Retry-After"))

	var result map[string]any

	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "another update is already running", result["error"])
	assert.Equal(t, "v1", result["api_version"])
}

func TestHandler_Handle_TargetedUpdateBlocks(t *testing.T) {
	var called atomic.Int32

	updateFn := func(_ context.Context, _, _ []string) *metrics.Metric {
		called.Add(1)

		return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
	}

	lock := make(chan bool, 1)
	h := New(updateFn, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	done := make(chan int, 1)

	go func() {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/update?image=myimage:latest", nil)

		resp, err := app.Test(req)
		if err != nil {
			done <- 0

			return
		}
		defer resp.Body.Close()

		done <- resp.StatusCode
	}()

	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, int32(0), called.Load())

	lock <- true

	time.Sleep(50 * time.Millisecond)

	code := <-done
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, int32(1), called.Load())
}

func TestHandler_Handle_TargetedContainerUpdateBlocks(t *testing.T) {
	var called atomic.Int32

	updateFn := func(_ context.Context, _, _ []string) *metrics.Metric {
		called.Add(1)

		return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
	}

	lock := make(chan bool, 1)
	h := New(updateFn, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	done := make(chan int, 1)

	go func() {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/update?container=mycontainer", nil)

		resp, err := app.Test(req)
		if err != nil {
			done <- 0

			return
		}
		defer resp.Body.Close()

		done <- resp.StatusCode
	}()

	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, int32(0), called.Load())

	lock <- true

	time.Sleep(50 * time.Millisecond)

	code := <-done
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, int32(1), called.Load())
}

func TestHandler_Handle_ContextCancellation(t *testing.T) {
	lock := make(chan bool, 1)
	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return &metrics.Metric{}
	}, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan int, 1)

	go func() {
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/update?image=test:latest", strings.NewReader("test body"))

		resp, err := app.Test(req)
		if err != nil {
			done <- 0

			return
		}
		defer resp.Body.Close()

		done <- resp.StatusCode
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	code := <-done
	assert.True(t, code == http.StatusServiceUnavailable || code == 0,
		"expected 503 or 0, got %d", code)
}

func TestHandler_Handle_PanicRecovery(t *testing.T) {
	lock := make(chan bool, 1)
	lock <- true

	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		panic("simulated panic")
	}, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/update?async=true", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	time.Sleep(50 * time.Millisecond)

	select {
	case <-lock:
	default:
		t.Error("lock was not released after panic")
	}
}

func TestExtractImages_Unit(t *testing.T) {
	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return &metrics.Metric{}
	}, nil)

	tests := []struct {
		name      string
		query     string
		wantLen   int
		wantFirst string
	}{
		{
			name:      "single image",
			query:     "image=nginx:latest",
			wantLen:   1,
			wantFirst: "nginx:latest",
		},
		{
			name:      "comma-separated images",
			query:     "image=nginx:latest,redis:7",
			wantLen:   2,
			wantFirst: "nginx:latest",
		},
		{
			name:      "multiple image params",
			query:     "image=nginx:latest&image=redis:7",
			wantLen:   2,
			wantFirst: "nginx:latest",
		},
		{
			name:      "mixed comma and multiple params",
			query:     "image=nginx:latest,redis:7&image=postgres:16",
			wantLen:   3,
			wantFirst: "nginx:latest",
		},
		{
			name:    "no image param",
			query:   "other=value",
			wantLen: 0,
		},
		{
			name:    "empty query",
			query:   "",
			wantLen: 0,
		},
		{
			name:    "empty image value returns one empty part",
			query:   "image=",
			wantLen: 1,
		},
		{
			name:    "only commas produces empty parts",
			query:   "image=,,,",
			wantLen: 4,
		},
		{
			name:      "image with registry path",
			query:     "image=registry.com/org/image:v1.2.3",
			wantLen:   1,
			wantFirst: "registry.com/org/image:v1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedImages []string

			app := fiber.New(fiber.Config{})
			app.Get("/test", func(c fiber.Ctx) error {
				capturedImages = h.extractImages(c)

				return nil
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test?"+tt.query, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Len(t, capturedImages, tt.wantLen, "image count mismatch")

			if tt.wantLen > 0 {
				assert.Equal(t, tt.wantFirst, capturedImages[0], "first image mismatch")
			}
		})
	}
}

func TestExtractContainers_Unit(t *testing.T) {
	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return &metrics.Metric{}
	}, nil)

	tests := []struct {
		name      string
		query     string
		wantLen   int
		wantFirst string
	}{
		{
			name:      "single container",
			query:     "container=mycontainer",
			wantLen:   1,
			wantFirst: "mycontainer",
		},
		{
			name:      "comma-separated containers",
			query:     "container=container1,container2",
			wantLen:   2,
			wantFirst: "container1",
		},
		{
			name:      "multiple container params",
			query:     "container=container1&container=container2",
			wantLen:   2,
			wantFirst: "container1",
		},
		{
			name:      "regex pattern",
			query:     "container=^web-.*",
			wantLen:   1,
			wantFirst: "^web-.*",
		},
		{
			name:    "no container param",
			query:   "other=value",
			wantLen: 0,
		},
		{
			name:    "empty query",
			query:   "",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedContainers []string

			app := fiber.New(fiber.Config{})
			app.Get("/test", func(c fiber.Ctx) error {
				capturedContainers = h.extractContainers(c)

				return nil
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test?"+tt.query, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Len(t, capturedContainers, tt.wantLen, "container count mismatch")

			if tt.wantLen > 0 {
				assert.Equal(t, tt.wantFirst, capturedContainers[0], "first container mismatch")
			}
		})
	}
}

func TestContainerFilter(t *testing.T) {
	tests := []struct {
		name      string
		patterns  []string
		inputName string
		wantMatch bool
	}{
		{
			name:      "exact match",
			patterns:  []string{"mycontainer"},
			inputName: "mycontainer",
			wantMatch: true,
		},
		{
			name:      "exact mismatch",
			patterns:  []string{"othercontainer"},
			inputName: "mycontainer",
			wantMatch: false,
		},
		{
			name:      "regex match with prefix",
			patterns:  []string{"^web-.*"},
			inputName: "web-server",
			wantMatch: true,
		},
		{
			name:      "regex match with suffix",
			patterns:  []string{".*-prod$"},
			inputName: "api-prod",
			wantMatch: true,
		},
		{
			name:      "regex no match",
			patterns:  []string{"^web-.*"},
			inputName: "api-server",
			wantMatch: false,
		},
		{
			name:      "multiple patterns first matches",
			patterns:  []string{"^web-.*", "^api-.*"},
			inputName: "web-server",
			wantMatch: true,
		},
		{
			name:      "multiple patterns second matches",
			patterns:  []string{"^web-.*", "^api-.*"},
			inputName: "api-server",
			wantMatch: true,
		},
		{
			name:      "multiple patterns none match",
			patterns:  []string{"^web-.*", "^api-.*"},
			inputName: "db-server",
			wantMatch: false,
		},
		{
			name:      "empty patterns",
			patterns:  nil,
			inputName: "anything",
			wantMatch: false,
		},
		{
			name:      "invalid regex falls back to exact",
			patterns:  []string{"[invalid"},
			inputName: "[invalid",
			wantMatch: true,
		},
		{
			name:      "invalid regex no match",
			patterns:  []string{"[invalid"},
			inputName: "other",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := ContainerFilter(tt.patterns)
			got := filter(tt.inputName, true)
			assert.Equal(t, tt.wantMatch, got)
		})
	}
}

func Test_send429Response(t *testing.T) {
	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return &metrics.Metric{}
	}, nil)

	app := fiber.New(fiber.Config{})
	app.Post("/test", func(c fiber.Ctx) error {
		h.send429Response(c)

		return nil
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "30", resp.Header.Get("Retry-After"))

	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	assert.Contains(t, body, "another update is already running")
	assert.Contains(t, body, "api_version")
	assert.Contains(t, body, "v1")
}

func Test_executeUpdate(t *testing.T) {
	expected := &metrics.Metric{Scanned: 10, Updated: 5, Failed: 2}

	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return expected
	}, nil)

	metric, duration := h.executeUpdate(t.Context(), []string{"nginx:latest"}, nil)
	assert.Equal(t, expected, metric)
	assert.GreaterOrEqual(t, duration, time.Duration(0))
}

func Test_executeUpdateAsync(t *testing.T) {
	var called atomic.Int32

	lock := make(chan bool, 1)
	lock <- true

	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		called.Add(1)

		return &metrics.Metric{}
	}, lock)

	token := <-lock
	h.executeUpdateAsync(t.Context(), nil, nil, token)

	assert.Equal(t, int32(1), called.Load())

	select {
	case <-lock:
	default:
		t.Error("lock was not released")
	}
}

func Test_releaseLock(t *testing.T) {
	lock := make(chan bool, 1)
	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return &metrics.Metric{}
	}, lock)

	h.releaseLock(true)

	select {
	case token := <-lock:
		assert.True(t, token)
	default:
		t.Error("lock should have a token after releaseLock")
	}
}

func Test_handleSync(t *testing.T) {
	expected := &metrics.Metric{
		Scanned: 8, Updated: 3, Failed: 1, Restarted: 2, Skipped: 1,
	}

	lock := make(chan bool, 1)
	lock <- true

	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return expected
	}, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/update", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	assert.Contains(t, body, "summary")
	assert.Contains(t, body, "timing")
	assert.Contains(t, body, "scanned")
	assert.Contains(t, body, "api_version")
}

func Test_handleSync_JSONStructure(t *testing.T) {
	expected := &metrics.Metric{
		Scanned: 10, Updated: 3, Failed: 1, Restarted: 2, Skipped: 0,
	}

	lock := make(chan bool, 1)
	lock <- true

	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return expected
	}, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/v1/update", h.Handle)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/update", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	var result map[string]any

	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	summary := result["summary"].(map[string]any)
	assert.InDelta(t, float64(10), summary["scanned"], 0.001)
	assert.InDelta(t, float64(3), summary["updated"], 0.001)
	assert.InDelta(t, float64(1), summary["failed"], 0.001)
	assert.InDelta(t, float64(2), summary["restarted"], 0.001)
	assert.InDelta(t, float64(0), summary["skipped"], 0.001)

	timing := result["timing"].(map[string]any)
	assert.NotNil(t, timing["duration_ms"])
	assert.NotNil(t, timing["duration"])

	assert.NotEmpty(t, result["timestamp"])
	assert.Equal(t, "v1", result["api_version"])
}

func Test_handleAsync(t *testing.T) {
	lock := make(chan bool, 1)
	lock <- true

	h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
		return &metrics.Metric{}
	}, lock)

	app := fiber.New(fiber.Config{})
	app.Post("/test", func(c fiber.Ctx) error {
		return h.handleAsync(c, nil, nil, true)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusAccepted, resp.StatusCode)

	select {
	case <-lock:
	default:
		t.Error("lock should be released after async update")
	}
}
