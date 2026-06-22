package check

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
			h := New(tt.check)
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
			h := New(tt.checkFunc)
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
	}{
		{
			name:       "with image filter",
			query:      "?image=nginx",
			wantStatus: http.StatusOK,
		},
		{
			name:       "with name filter",
			query:      "?name=my-container",
			wantStatus: http.StatusOK,
		},
		{
			name:       "with multiple filters",
			query:      "?image=nginx&name=my-container",
			wantStatus: http.StatusOK,
		},
		{
			name:       "with comma-separated filters",
			query:      "?name=container1,container2",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(func(ctx context.Context, images, names []string) ([]ContainerCheck, error) {
				return []ContainerCheck{}, nil
			})
			app := fiber.New(fiber.Config{})
			app.Post("/v1/check", h.Handle)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/check"+tt.query, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}
