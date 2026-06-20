package images

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	containermocks "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	typemocks "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestNew(t *testing.T) {
	h := New(func(_ context.Context) ([]ImageStatus, error) { return nil, nil })
	require.NotNil(t, h)
	assert.Equal(t, "/v1/images", h.Path)
}

func TestHandler_Handle(t *testing.T) {
	tests := []struct {
		name       string
		listFunc   func(ctx context.Context) ([]ImageStatus, error)
		wantStatus int
	}{
		{
			name: "successful list returns 200",
			listFunc: func(_ context.Context) ([]ImageStatus, error) {
				return []ImageStatus{
					{Name: "nginx:latest", ImageID: "sha256:abc", Digest: "sha256:def", Containers: 2},
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "empty list returns 200",
			listFunc: func(_ context.Context) ([]ImageStatus, error) {
				return []ImageStatus{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "list error returns 500",
			listFunc: func(_ context.Context) ([]ImageStatus, error) {
				return nil, errors.New("docker error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.listFunc)
			app := fiber.New(fiber.Config{})
			app.Get("/v1/images", h.Handle)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/images", nil)
			resp, err := app.Test(req)
			require.NoError(t, err)

			defer resp.Body.Close()

			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}

func TestListImageStatuses(t *testing.T) {
	tests := []struct {
		name    string
		client  func(t *testing.T) *containermocks.MockClient
		filter  types.Filter
		wantErr bool
		wantLen int
	}{
		{
			name: "successful list with single image",
			client: func(t *testing.T) *containermocks.MockClient {
				t.Helper()
				c := containermocks.NewMockClient(t)
				container := typemocks.NewMockContainer(t)
				container.EXPECT().ImageName().Return("nginx:latest")
				container.EXPECT().ImageID().Return(types.ImageID("sha256:abc123"))
				container.EXPECT().ImageInfo().Return(nil)
				c.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{container}, nil)

				return c
			},
			filter:  nil,
			wantErr: false,
			wantLen: 1,
		},
		{
			name: "multiple containers same image",
			client: func(t *testing.T) *containermocks.MockClient {
				t.Helper()
				c := containermocks.NewMockClient(t)

				container1 := typemocks.NewMockContainer(t)
				container1.EXPECT().ImageName().Return("nginx:latest")
				container1.EXPECT().ImageID().Return(types.ImageID("sha256:abc123"))
				container1.EXPECT().ImageInfo().Return(nil)

				container2 := typemocks.NewMockContainer(t)
				container2.EXPECT().ImageName().Return("nginx:latest")
				container2.EXPECT().ImageID().Return(types.ImageID("sha256:abc123"))

				c.EXPECT().ListContainers(mock.Anything).Return([]types.Container{container1, container2}, nil)

				return c
			},
			filter:  nil,
			wantErr: false,
			wantLen: 1,
		},
		{
			name: "client error returns wrapped error",
			client: func(t *testing.T) *containermocks.MockClient {
				t.Helper()
				c := containermocks.NewMockClient(t)
				c.EXPECT().ListContainers(mock.Anything).Return(nil, errors.New("connection refused"))

				return c
			},
			filter:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.client(t)
			statuses, err := ListImageStatuses(t.Context(), client, tt.filter)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, statuses, tt.wantLen)
			}
		})
	}
}

func Test_filterImages(t *testing.T) {
	statuses := []ImageStatus{
		{Name: "nginx:latest", ImageID: "sha256:abc"},
		{Name: "redis:latest", ImageID: "sha256:def"},
	}

	tests := []struct {
		name    string
		nameF   string
		idF     string
		wantLen int
	}{
		{name: "no filter", nameF: "", idF: "", wantLen: 2},
		{name: "filter by name", nameF: "nginx:latest", idF: "", wantLen: 1},
		{name: "filter by id", nameF: "", idF: "sha256:abc", wantLen: 1},
		{name: "no match", nameF: "postgres:latest", idF: "", wantLen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterImages(statuses, tt.nameF, tt.idF)
			assert.Len(t, result, tt.wantLen)
		})
	}
}
