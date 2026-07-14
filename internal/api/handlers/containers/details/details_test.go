package details

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestGetContainerDetails(t *testing.T) {
	tests := []struct {
		name    string
		client  func(t *testing.T) *mockContainer.MockClient
		filter  types.Filter
		wantErr bool
		wantLen int
	}{
		{
			name: "successful list with container",
			client: func(t *testing.T) *mockContainer.MockClient {
				t.Helper()
				c := mockContainer.NewMockClient(t)
				container := mockTypes.NewMockContainer(t)
				container.EXPECT().Name().Return("test-container")
				container.EXPECT().ImageName().Return("nginx:latest")
				container.EXPECT().ImageID().Return(types.ImageID("sha256:abc123"))
				container.EXPECT().IsRunning().Return(true)
				container.EXPECT().IsWatchtower().Return(false)
				container.EXPECT().IsMonitorOnly(mock.Anything).Return(false)
				container.EXPECT().IsNoPull(mock.Anything).Return(false)
				container.EXPECT().Enabled().Return(true, true)
				container.EXPECT().IsStale().Return(false)
				container.EXPECT().Scope().Return("", true)
				container.EXPECT().ImageInfo().Return(nil)
				c.EXPECT().ListContainers(mock.Anything).Return([]types.Container{container}, nil)

				return c
			},
			filter:  nil,
			wantErr: false,
			wantLen: 1,
		},
		{
			name: "client error returns wrapped error",
			client: func(t *testing.T) *mockContainer.MockClient {
				t.Helper()
				c := mockContainer.NewMockClient(t)
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
			details, err := GetContainerDetails(t.Context(), client, tt.filter, "", "", types.UpdateParams{})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, details, tt.wantLen)
			}
		})
	}
}
