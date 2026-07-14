package config

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
	h := New(func(_ context.Context) (ConfigData, error) {
		return ConfigData{}, nil
	})
	require.NotNil(t, h)
	assert.Equal(t, "/v1/config", h.Path)
}

func TestHandler_Handle(t *testing.T) {
	tests := []struct {
		name       string
		getConfig  func(ctx context.Context) (ConfigData, error)
		wantStatus int
	}{
		{
			name: "successful config returns 200",
			getConfig: func(_ context.Context) (ConfigData, error) {
				return ConfigData{
					MonitorOnly: true,
					Cleanup:     true,
					FilterDesc:  "all containers",
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "config error returns 500",
			getConfig: func(_ context.Context) (ConfigData, error) {
				return ConfigData{}, errors.New("config error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.getConfig)
			app := fiber.New(fiber.Config{})
			app.Get("/v1/config", h.Handle)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/config", nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}
