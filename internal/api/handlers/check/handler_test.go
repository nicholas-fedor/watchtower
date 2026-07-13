package check

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name  string
		check CheckFunc
	}{
		{
			name:  "with check function",
			check: func(_ context.Context, _, _ []string) ([]ContainerCheck, error) { return nil, nil },
		},
		{
			name:  "with nil check function",
			check: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.check, 5*time.Minute, nil, false)
			require.NotNil(t, h)
			assert.Equal(t, "/v1/check", h.Path)
		})
	}
}

func TestHandler_Handle(t *testing.T) {
	tests := []struct {
		name       string
		checkFunc  CheckFunc
		wantStatus int
	}{
		{
			name: "successful check returns 200",
			checkFunc: func(_ context.Context, _, _ []string) ([]ContainerCheck, error) {
				return []ContainerCheck{
					{Name: "container1", Image: "nginx:latest", ImageID: "sha256:abc", UpdateAvailable: true},
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "empty results returns 200",
			checkFunc: func(_ context.Context, _, _ []string) ([]ContainerCheck, error) {
				return []ContainerCheck{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "check error returns 500",
			checkFunc: func(_ context.Context, _, _ []string) ([]ContainerCheck, error) {
				return nil, errors.New("docker error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.checkFunc, 5*time.Minute, nil, false)
			app := fiber.New(fiber.Config{})
			app.Post("/v1/check", h.Handle)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/check", nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

func TestHandler_Handle_WithFilters(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantImages []string
		wantNames  []string
	}{
		{
			name:       "with image filter",
			query:      "?image=nginx",
			wantStatus: http.StatusOK,
			wantImages: []string{"nginx"},
		},
		{
			name:       "with name filter",
			query:      "?container=my-container",
			wantStatus: http.StatusOK,
			wantNames:  []string{"my-container"},
		},
		{
			name:       "with multiple filters",
			query:      "?image=nginx&container=my-container",
			wantStatus: http.StatusOK,
			wantImages: []string{"nginx"},
			wantNames:  []string{"my-container"},
		},
		{
			name:       "with comma-separated filters",
			query:      "?container=container1,container2",
			wantStatus: http.StatusOK,
			wantNames:  []string{"container1", "container2"},
		},
		{
			name:       "with comma-separated image filter",
			query:      "?image=nginx,redis",
			wantStatus: http.StatusOK,
			wantImages: []string{"nginx", "redis"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedImages, capturedNames []string

			h := New(func(ctx context.Context, images, names []string) ([]ContainerCheck, error) {
				capturedImages = images
				capturedNames = names

				return []ContainerCheck{}, nil
			}, 5*time.Minute, nil, false)
			app := fiber.New(fiber.Config{})
			app.Post("/v1/check", h.Handle)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/check"+tt.query, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)

			if tt.wantImages != nil {
				assert.Equal(t, tt.wantImages, capturedImages)
			}

			if tt.wantNames != nil {
				assert.Equal(t, tt.wantNames, capturedNames)
			}
		})
	}
}

func TestHandler_Handle_TimeoutOverride(t *testing.T) {
	t.Run("valid timeout is applied", func(t *testing.T) {
		h := New(func(ctx context.Context, _, _ []string) ([]ContainerCheck, error) {
			return []ContainerCheck{}, nil
		}, 5*time.Minute, nil, false)
		app := fiber.New(fiber.Config{})
		app.Post("/v1/check", h.Handle)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/check?timeout=2m", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("timeout exceeding max is clamped", func(t *testing.T) {
		h := New(func(ctx context.Context, _, _ []string) ([]ContainerCheck, error) {
			return []ContainerCheck{}, nil
		}, 2*time.Minute, nil, false)
		app := fiber.New(fiber.Config{})
		app.Post("/v1/check", h.Handle)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/check?timeout=5m", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("invalid timeout is ignored", func(t *testing.T) {
		h := New(func(ctx context.Context, _, _ []string) ([]ContainerCheck, error) {
			return []ContainerCheck{}, nil
		}, 5*time.Minute, nil, false)
		app := fiber.New(fiber.Config{})
		app.Post("/v1/check", h.Handle)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/check?timeout=bogus", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
