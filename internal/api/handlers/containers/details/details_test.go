package details

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockDetails "github.com/nicholas-fedor/watchtower/internal/api/handlers/containers/details/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// TestGetContainerDetails verifies that each listed container's details carry
// its identity, state and labels, with the enable label weighed against the
// label-enable setting.
func TestGetContainerDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		containers []*container.Container
		listErr    error
		params     updateParams.Params
		wantLen    int
		want       func(t *testing.T, details []ContainerDetails)
	}{
		{
			name: "successful list with labeled enabled container",
			containers: []*container.Container{
				modelfixture.Container(
					containerfixture.WithName("test-container"),
					containerfixture.WithImageName("nginx:latest"),
					containerfixture.WithImageID("sha256:abc123"),
					containerfixture.WithLabels(map[string]string{
						labels.Enable: "true",
						labels.Scope:  "prod",
					}),
				),
			},
			params:  updateParams.Params{LabelEnable: true},
			wantLen: 1,
			want: func(t *testing.T, details []ContainerDetails) {
				t.Helper()
				assert.Equal(t, ContainerDetails{
					Name:        "test-container",
					Image:       "nginx:latest",
					ImageID:     "sha256:abc123",
					Digest:      "",
					Scope:       "prod",
					Running:     true,
					Watchtower:  false,
					MonitorOnly: false,
					NoPull:      false,
					Enabled:     true,
					Stale:       false,
				}, details[0])
			},
		},
		{
			name: "unlabeled container is enabled when label-enable is false",
			containers: []*container.Container{
				modelfixture.Container(
					containerfixture.WithName("unlabeled-container"),
					containerfixture.WithImageName("nginx:latest"),
				),
			},
			params:  updateParams.Params{LabelEnable: false},
			wantLen: 1,
			want: func(t *testing.T, details []ContainerDetails) {
				t.Helper()
				assert.True(t, details[0].Enabled)
			},
		},
		{
			name: "unlabeled container is disabled when label-enable is true",
			containers: []*container.Container{
				modelfixture.Container(
					containerfixture.WithName("unlabeled-container"),
					containerfixture.WithImageName("nginx:latest"),
				),
			},
			params:  updateParams.Params{LabelEnable: true},
			wantLen: 1,
			want: func(t *testing.T, details []ContainerDetails) {
				t.Helper()
				assert.False(t, details[0].Enabled)
			},
		},
		{
			name: "labeled false container is disabled when label-enable is false",
			containers: []*container.Container{
				modelfixture.Container(
					containerfixture.WithName("disabled-container"),
					containerfixture.WithImageName("nginx:latest"),
					containerfixture.WithLabels(map[string]string{labels.Enable: "false"}),
				),
			},
			params:  updateParams.Params{LabelEnable: false},
			wantLen: 1,
			want: func(t *testing.T, details []ContainerDetails) {
				t.Helper()
				assert.False(t, details[0].Enabled)
			},
		},
		{
			name: "digest from the container's repository when the image has several",
			containers: []*container.Container{
				modelfixture.Container(
					containerfixture.WithName("app"),
					containerfixture.WithImageName("ghcr.io/org/app:latest"),
					containerfixture.WithRepoDigests([]string{"docker.io/org/app@sha256:hub", "ghcr.io/org/app@sha256:ghcr"}),
				),
			},
			params:  updateParams.Params{},
			wantLen: 1,
			want: func(t *testing.T, details []ContainerDetails) {
				t.Helper()
				assert.Equal(t, "sha256:ghcr", details[0].Digest)
			},
		},
		{
			name:    "client error returns wrapped error",
			listErr: errors.New("connection refused"),
			params:  updateParams.Params{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := mockDetails.NewMockContainerLister(t)
			client.EXPECT().ListContainers(mock.Anything).Return(tt.containers, tt.listErr)

			details, err := GetContainerDetails(t.Context(), client, nil, "", "", tt.params)

			if tt.listErr != nil {
				require.ErrorIs(t, err, tt.listErr)
				assert.ErrorContains(t, err, "failed to list containers")
			} else {
				require.NoError(t, err)
				assert.Len(t, details, tt.wantLen)

				if tt.want != nil {
					tt.want(t, details)
				}
			}
		})
	}
}
