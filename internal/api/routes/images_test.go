package routes

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestRegisterImagesRoute(t *testing.T) {
	t.Run("with client registers route", func(t *testing.T) {
		app := testApp()
		auth := testAuthMiddleware()
		mockClient := mockContainer.NewMockClient(t)
		mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{}, nil).Maybe()

		opts := Options{
			EnableImagesAPI: true,
			Client:          mockClient,
			Filter:          func(_ types.FilterableContainer) bool { return true },
		}

		registerImagesRoute(app, auth, opts)

		routes := app.GetRoutes()
		found := false

		for _, r := range routes {
			if r.Path == "/v1/images" && r.Method == http.MethodGet {
				found = true

				break
			}
		}

		assert.True(t, found, "GET /v1/images should be registered")
	})

	t.Run("nil client skips route", func(t *testing.T) {
		app := testApp()
		auth := testAuthMiddleware()

		opts := Options{
			EnableImagesAPI: true,
			Client:          nil,
			Filter:          func(_ types.FilterableContainer) bool { return true },
		}

		registerImagesRoute(app, auth, opts)

		routes := app.GetRoutes()

		for _, r := range routes {
			if r.Path == "/v1/images" {
				t.Error("GET /v1/images should not be registered when client is nil")
			}
		}
	})
}
