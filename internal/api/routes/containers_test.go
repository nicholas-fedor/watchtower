package routes

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestRegisterContainersRoute(t *testing.T) {
	app := testApp()
	auth := testAuthMiddleware()
	mockClient := mockContainer.NewMockClient(t)
	mockClient.EXPECT().ListContainers(mock.Anything).Return([]types.Container{}, nil).Maybe()

	opts := config.Options{
		EnableContainersAPI: true,
		Client:              mockClient,
		UnblockHTTPAPI:      true,
	}

	registerContainersRoute(app, auth, opts)

	routes := app.GetRoutes()
	found := false

	for _, r := range routes {
		if r.Path == "/v1/containers" && r.Method == http.MethodGet {
			found = true

			break
		}
	}

	assert.True(t, found, "GET /v1/containers should be registered")
}
