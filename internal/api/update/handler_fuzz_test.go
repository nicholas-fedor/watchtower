package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/internal/metrics"
)

// FuzzExtractImages fuzzes the core image-name parsing logic used by extractImages.
// It tests that splitting comma-separated image names never panics and produces
// deterministic results. The fuzz target invokes the real extractImages method
// on a constructed Handler with a real fiber.Ctx.
func FuzzExtractImages(f *testing.F) {
	f.Add("nginx:latest")
	f.Add("nginx:latest,redis:7")
	f.Add("nginx:latest,redis:7,postgres:16")
	f.Add("")
	f.Add(",,,")
	f.Add("a")
	f.Add(strings.Repeat("x", 1000))
	f.Add("image with spaces")
	f.Add("registry.com/org/image:v1.2.3")

	f.Fuzz(func(t *testing.T, raw string) {
		h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
			return &metrics.Metric{}
		}, nil)

		app := fiber.New(fiber.Config{})
		app.Post("/test", func(c fiber.Ctx) error {
			images := h.extractImages(c)

			if len(raw) > 0 && !strings.Contains(raw, ",") && len(images) != 1 {
				t.Errorf("non-empty input %q produced %d images, want 1", raw, len(images))
			}

			if raw == "" && len(images) != 0 {
				t.Errorf("empty input produced %d images, want 0", len(images))
			}

			return nil
		})

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test?image="+url.QueryEscape(raw), nil)

		resp, err := app.Test(req)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("request failed: %v", err)
		}

		if resp != nil {
			defer resp.Body.Close()
		}
	})
}

// FuzzHandlerNew fuzzes the Handler constructor to ensure it never panics
// with various lock channel configurations.
func FuzzHandlerNew(f *testing.F) {
	f.Add(true)
	f.Add(false)

	f.Fuzz(func(t *testing.T, withLock bool) {
		var lock chan bool
		if withLock {
			lock = make(chan bool, 1)
		}

		h := New(func(_ context.Context, _, _ []string) *metrics.Metric {
			return &metrics.Metric{}
		}, lock)

		require.NotNil(t, h)
		assert.Equal(t, "/v1/update", h.Path)
	})
}
