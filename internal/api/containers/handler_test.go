package containers

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
		name string
		list ListFunc
	}{
		{
			name: "with list function",
			list: func(_ context.Context) ([]Status, error) { return nil, nil },
		},
		{
			name: "with nil list function",
			list: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.list)
			require.NotNil(t, h)
			assert.Equal(t, "/v1/containers", h.Path)
		})
	}
}

func TestHandler_Handle(t *testing.T) {
	tests := []struct {
		name       string
		listFunc   ListFunc
		wantStatus int
	}{
		{
			name: "successful list returns 200",
			listFunc: func(_ context.Context) ([]Status, error) {
				return []Status{
					{Name: "container1", Image: "nginx:latest", ImageID: "sha256:abc", Digest: "sha256:def"},
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "empty list returns 200",
			listFunc: func(_ context.Context) ([]Status, error) {
				return []Status{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list error returns 500",
			listFunc: func(_ context.Context) ([]Status, error) {
				return nil, errors.New("docker error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.listFunc)
			app := fiber.New(fiber.Config{})
			app.Get("/v1/containers", h.Handle)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/containers", nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}
