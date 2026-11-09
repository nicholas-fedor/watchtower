package containers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockContainers "github.com/nicholas-fedor/watchtower/internal/api/handlers/containers/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/filters"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// nginxContainer returns a running container with the given name that runs
// nginx:latest, whose image has the ID sha256:abc123 and the given repository
// digests.
func nginxContainer(name string, repoDigests ...string) *container.Container {
	return modelfixture.Container(
		containerfixture.WithName(name),
		containerfixture.WithImageName("nginx:latest"),
		containerfixture.WithImageID("sha256:abc123"),
		containerfixture.WithRepoDigests(repoDigests),
	)
}

// TestListContainerStatuses verifies that every listed container gets a status,
// that a filter reaches the listing unchanged, and that a listing failure is
// returned wrapped.
func TestListContainerStatuses(t *testing.T) {
	t.Parallel()

	// The filter case keeps the first container and drops the second, so the
	// expectation can tell the forwarded filter from any other.
	kept := nginxContainer("kept")
	dropped := nginxContainer("dropped")

	tests := []struct {
		name       string
		containers []*container.Container
		listErr    error
		filter     filters.Filter
		wantLen    int
	}{
		{
			name: "successful list with containers",
			containers: []*container.Container{
				nginxContainer("test-container"),
			},
			wantLen: 1,
		},
		{
			name:    "client error returns wrapped error",
			listErr: errors.New("connection refused"),
		},
		{
			name:       "empty container list",
			containers: []*container.Container{},
			wantLen:    0,
		},
		{
			name:       "non-nil filter is passed through to ListContainers",
			containers: []*container.Container{kept},
			filter:     func(c filters.FilterableContainer) bool { return c.Name() == kept.Name() },
			wantLen:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := mockContainers.NewMockContainerLister(t)

			if tt.filter != nil {
				// The mock records the variadic filters as one slice.
				forwarded := mock.MatchedBy(func(received []filters.Filter) bool {
					return len(received) == 1 && received[0](kept) && !received[0](dropped)
				})
				client.EXPECT().ListContainers(mock.Anything, forwarded).Return(tt.containers, tt.listErr)
			} else {
				client.EXPECT().ListContainers(mock.Anything).Return(tt.containers, tt.listErr)
			}

			statuses, err := ListContainerStatuses(t.Context(), client, tt.filter)

			if tt.listErr != nil {
				require.ErrorIs(t, err, tt.listErr)
				assert.ErrorContains(t, err, "failed to list containers")
			} else {
				require.NoError(t, err)
				assert.Len(t, statuses, tt.wantLen)
			}
		})
	}
}

// Test_containerToStatus verifies that a status carries the container's name,
// image and image ID, with the digest of the container's own image repository.
func Test_containerToStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		running *container.Container
		want    Status
	}{
		{
			name:    "container with image info but no repo digests",
			running: nginxContainer("my-container"),
			want: Status{
				Name:    "my-container",
				Image:   "nginx:latest",
				ImageID: "sha256:abc123",
				Digest:  "",
			},
		},
		{
			name:    "digest from the container's repository when the image has several",
			running: nginxContainer("my-container", "ghcr.io/org/nginx@sha256:ghcr", "docker.io/library/nginx@sha256:hub"),
			want: Status{
				Name:    "my-container",
				Image:   "nginx:latest",
				ImageID: "sha256:abc123",
				Digest:  "sha256:hub",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := containerToStatus(tt.running)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_filterStatuses(t *testing.T) {
	tests := []struct {
		name        string
		statuses    []Status
		nameFilter  string
		imageFilter string
		wantLen     int
	}{
		{
			name: "no filters returns all",
			statuses: []Status{
				{Name: "app1", Image: "nginx:latest"},
				{Name: "app2", Image: "redis:7"},
			},
			nameFilter:  "",
			imageFilter: "",
			wantLen:     2,
		},
		{
			name: "filter by name",
			statuses: []Status{
				{Name: "app1", Image: "nginx:latest"},
				{Name: "app2", Image: "redis:7"},
			},
			nameFilter:  "app1",
			imageFilter: "",
			wantLen:     1,
		},
		{
			name: "filter by image",
			statuses: []Status{
				{Name: "app1", Image: "nginx:latest"},
				{Name: "app2", Image: "redis:7"},
			},
			nameFilter:  "",
			imageFilter: "redis:7",
			wantLen:     1,
		},
		{
			name: "filter by both name and image",
			statuses: []Status{
				{Name: "app1", Image: "nginx:latest"},
				{Name: "app2", Image: "redis:7"},
			},
			nameFilter:  "app1",
			imageFilter: "nginx:latest",
			wantLen:     1,
		},
		{
			name: "no match returns empty",
			statuses: []Status{
				{Name: "app1", Image: "nginx:latest"},
			},
			nameFilter:  "nonexistent",
			imageFilter: "",
			wantLen:     0,
		},
		{
			name:        "empty statuses returns empty",
			statuses:    []Status{},
			nameFilter:  "",
			imageFilter: "",
			wantLen:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterStatuses(tt.statuses, tt.nameFilter, tt.imageFilter)
			assert.NotNil(t, got, "an empty result encodes as a JSON array, not null")
			assert.Len(t, got, tt.wantLen)
		})
	}
}
