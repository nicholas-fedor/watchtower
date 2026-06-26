package check

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/moby/moby/api/types/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestCheckForUpdates(t *testing.T) {
	tests := []struct {
		name      string
		client    func(t *testing.T) *mockContainer.MockClient
		images    []string
		names     []string
		wantErr   bool
		errMsg    string
		wantLen   int
		wantStale []bool
		wantNames []string
	}{
		{
			name: "single container with update available",
			client: func(t *testing.T) *mockContainer.MockClient {
				t.Helper()
				c := mockContainer.NewMockClient(t)
				container := mockTypes.NewMockContainer(t)
				container.EXPECT().Name().Return("my-app")
				container.EXPECT().ImageName().Return("nginx:1.25")
				container.EXPECT().ImageID().Return(types.ImageID("sha256:abc"))
				container.EXPECT().ImageInfo().Return(nil)
				c.EXPECT().ListContainers(mock.Anything).Return([]types.Container{container}, nil)
				c.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
					Return(true, types.ImageID("sha256:def"), "", nil)

				return c
			},
			wantLen:   1,
			wantStale: []bool{true},
		},
		{
			name: "single container no update available",
			client: func(t *testing.T) *mockContainer.MockClient {
				t.Helper()
				c := mockContainer.NewMockClient(t)
				container := mockTypes.NewMockContainer(t)
				container.EXPECT().Name().Return("my-app")
				container.EXPECT().ImageName().Return("nginx:1.25")
				container.EXPECT().ImageID().Return(types.ImageID("sha256:abc"))
				container.EXPECT().ImageInfo().Return(nil)
				c.EXPECT().ListContainers(mock.Anything).Return([]types.Container{container}, nil)
				c.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
					Return(false, types.ImageID("sha256:abc"), "", nil)

				return c
			},
			wantLen:   1,
			wantStale: []bool{false},
		},
		{
			name: "filter by image name excludes non-matching",
			client: func(t *testing.T) *mockContainer.MockClient {
				t.Helper()
				c := mockContainer.NewMockClient(t)
				container1 := mockTypes.NewMockContainer(t)
				container1.EXPECT().Name().Return("app1").Maybe()
				container1.EXPECT().ImageName().Return("nginx:latest").Maybe()
				container1.EXPECT().ImageID().Return(types.ImageID("sha256:abc")).Maybe()
				container1.EXPECT().ImageInfo().Return(nil).Maybe()

				container2 := mockTypes.NewMockContainer(t)
				container2.EXPECT().Name().Return("app2").Maybe()
				container2.EXPECT().ImageName().Return("redis:latest").Maybe()
				c.EXPECT().ListContainers(mock.Anything).Return([]types.Container{container1, container2}, nil)
				c.EXPECT().IsContainerStale(mock.Anything, container1, mock.Anything).
					Return(false, types.ImageID("sha256:abc"), "", nil)

				return c
			},
			images:    []string{"nginx:latest"},
			wantLen:   1,
			wantNames: []string{"app1"},
		},
		{
			name: "filter by container name excludes non-matching",
			client: func(t *testing.T) *mockContainer.MockClient {
				t.Helper()
				c := mockContainer.NewMockClient(t)
				container1 := mockTypes.NewMockContainer(t)
				container1.EXPECT().Name().Return("app1").Maybe()
				container1.EXPECT().ImageName().Return("nginx:latest").Maybe()
				container1.EXPECT().ImageID().Return(types.ImageID("sha256:abc")).Maybe()
				container1.EXPECT().ImageInfo().Return(nil).Maybe()

				container2 := mockTypes.NewMockContainer(t)
				container2.EXPECT().Name().Return("app2").Maybe()
				container2.EXPECT().ImageName().Return("redis:latest").Maybe()
				c.EXPECT().ListContainers(mock.Anything).Return([]types.Container{container1, container2}, nil)
				c.EXPECT().IsContainerStale(mock.Anything, container1, mock.Anything).
					Return(false, types.ImageID("sha256:abc"), "", nil)

				return c
			},
			names:     []string{"app1"},
			wantLen:   1,
			wantNames: []string{"app1"},
		},
		{
			name: "empty container list",
			client: func(t *testing.T) *mockContainer.MockClient {
				t.Helper()
				c := mockContainer.NewMockClient(t)
				c.EXPECT().ListContainers(mock.Anything).Return([]types.Container{}, nil)

				return c
			},
			wantLen: 0,
		},
		{
			name: "list error returns wrapped error",
			client: func(t *testing.T) *mockContainer.MockClient {
				t.Helper()
				c := mockContainer.NewMockClient(t)
				c.EXPECT().ListContainers(mock.Anything).Return(nil, errors.New("connection refused"))

				return c
			},
			wantErr: true,
			errMsg:  "failed to list containers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.client(t)
			results, err := CheckForUpdates(t.Context(), client, nil, tt.images, tt.names)

			if tt.wantErr {
				require.Error(t, err)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.Len(t, results, tt.wantLen)

				if tt.wantStale != nil {
					for i, wantStale := range tt.wantStale {
						if i < len(results) {
							assert.Equal(t, wantStale, results[i].UpdateAvailable)
						}
					}
				}

				if tt.wantNames != nil {
					for i, wantName := range tt.wantNames {
						if i < len(results) {
							assert.Equal(t, wantName, results[i].Name)
						}
					}
				}
			}
		})
	}
}

func TestCheckForUpdates_DigestExtraction(t *testing.T) {
	client := mockContainer.NewMockClient(t)
	container := mockTypes.NewMockContainer(t)
	container.EXPECT().Name().Return("my-app")
	container.EXPECT().ImageName().Return("nginx:latest")
	container.EXPECT().ImageID().Return(types.ImageID("sha256:abc"))

	info := &image.InspectResponse{RepoDigests: []string{"nginx@sha256:digest123"}}
	container.EXPECT().ImageInfo().Return(info)
	client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{container}, nil)
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		Return(false, types.ImageID("sha256:abc"), "", nil)

	results, err := CheckForUpdates(t.Context(), client, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "sha256:digest123", results[0].Digest)
}

func TestCheckForUpdates_IsContainerStaleError(t *testing.T) {
	client := mockContainer.NewMockClient(t)
	container := mockTypes.NewMockContainer(t)
	container.EXPECT().Name().Return("my-app")
	container.EXPECT().ImageName().Return("nginx:latest")
	container.EXPECT().ImageID().Return(types.ImageID("sha256:abc"))
	container.EXPECT().ImageInfo().Return(nil)
	client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{container}, nil)
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		Return(false, types.ImageID(""), "", errors.New("registry unavailable"))

	results, err := CheckForUpdates(t.Context(), client, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "registry unavailable", results[0].Error)
	assert.False(t, results[0].UpdateAvailable)
}

func TestExtractFilterParams(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		key       string
		wantEmpty bool
		wantVals  []string
	}{
		{
			name:      "no params",
			query:     "",
			key:       "name",
			wantEmpty: true,
		},
		{
			name:     "single value",
			query:    "?name=my-container",
			key:      "name",
			wantVals: []string{"my-container"},
		},
		{
			name:     "comma-separated values",
			query:    "?name=container1,container2",
			key:      "name",
			wantVals: []string{"container1", "container2"},
		},
		{
			name:     "multiple params",
			query:    "?name=app1&name=app2",
			key:      "name",
			wantVals: []string{"app1", "app2"},
		},
		{
			name:      "empty value filtered out",
			query:     "?name=",
			key:       "name",
			wantEmpty: true,
		},
		{
			name:     "whitespace trimmed",
			query:    "?name=%20app1%20&name=app2",
			key:      "name",
			wantVals: []string{"app1", "app2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New(fiber.Config{})
			app.Get("/test", func(c fiber.Ctx) error {
				vals := extractFilterParams(c, tt.key)
				if tt.wantEmpty {
					assert.Empty(t, vals)
				} else {
					assert.Equal(t, tt.wantVals, vals)
				}

				return c.SendStatus(http.StatusOK)
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test"+tt.query, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}
