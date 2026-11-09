package images

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockImages "github.com/nicholas-fedor/watchtower/internal/api/handlers/images/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

// imageContainer returns a running container of the named image, whose image
// has the given ID and repository digests.
func imageContainer(imageName, imageID string, repoDigests ...string) *container.Container {
	return modelfixture.Container(
		containerfixture.WithImageName(imageName),
		containerfixture.WithImageID(imageID),
		containerfixture.WithRepoDigests(repoDigests),
	)
}

// TestListImageStatuses verifies that containers are grouped into one status
// per local image, with the digest of the container's own repository.
func TestListImageStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		containers []*container.Container
		listErr    error
		want       []ImageStatus
	}{
		{
			name:       "successful list with single image",
			containers: []*container.Container{imageContainer("nginx:latest", "sha256:abc123")},
			want: []ImageStatus{
				{Name: "nginx:latest", ImageID: "sha256:abc123", Containers: 1},
			},
		},
		{
			name: "multiple containers same image",
			containers: []*container.Container{
				imageContainer("nginx:latest", "sha256:abc123"),
				imageContainer("nginx:latest", "sha256:abc123"),
			},
			want: []ImageStatus{
				{Name: "nginx:latest", ImageID: "sha256:abc123", Containers: 2},
			},
		},
		{
			name:       "multiple repositories uses the container's repository",
			containers: []*container.Container{imageContainer("ghcr.io/org/app:latest", "sha256:abc123", "docker.io/org/app@sha256:hub", "ghcr.io/org/app@sha256:ghcr")},
			want: []ImageStatus{
				{Name: "ghcr.io/org/app:latest", ImageID: "sha256:abc123", Digest: "sha256:ghcr", Containers: 1},
			},
		},
		{
			name:    "client error returns wrapped error",
			listErr: errors.New("connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := mockImages.NewMockContainerLister(t)
			client.EXPECT().ListContainers(mock.Anything).Return(tt.containers, tt.listErr)

			statuses, err := ListImageStatuses(t.Context(), client, nil)

			if tt.listErr != nil {
				require.ErrorIs(t, err, tt.listErr)
				assert.ErrorContains(t, err, "failed to list containers")
			} else {
				require.NoError(t, err)
				assert.Len(t, statuses, len(tt.want))

				for i, want := range tt.want {
					assert.Equal(t, want.Name, statuses[i].Name)
					assert.Equal(t, want.ImageID, statuses[i].ImageID)
					assert.Equal(t, want.Digest, statuses[i].Digest)
					assert.Equal(t, want.Containers, statuses[i].Containers)
				}
			}
		})
	}
}

// TestListImageStatuses_SortsByNameThenImageID verifies that images are
// returned in a fixed order, by name and then by image ID for one name that
// points at several local images.
func TestListImageStatuses_SortsByNameThenImageID(t *testing.T) {
	t.Parallel()

	client := mockImages.NewMockContainerLister(t)
	client.EXPECT().ListContainers(mock.Anything).Return([]*container.Container{
		imageContainer("redis:7", "sha256:bbb"),
		imageContainer("nginx:1.27", "sha256:zzz"),
		imageContainer("ghcr.io/org/app:latest", "sha256:ccc"),
		imageContainer("nginx:1.27", "sha256:aaa"),
	}, nil)

	statuses, err := ListImageStatuses(t.Context(), client, nil)
	require.NoError(t, err)

	got := make([]string, 0, len(statuses))
	for _, status := range statuses {
		got = append(got, status.Name+"@"+status.ImageID)
	}

	assert.Equal(t, []string{
		"ghcr.io/org/app:latest@sha256:ccc",
		"nginx:1.27@sha256:aaa",
		"nginx:1.27@sha256:zzz",
		"redis:7@sha256:bbb",
	}, got)
}

// TestListImageStatuses_NameForSeveralTags verifies that containers running one
// image under several tags are reported under the same name in either list
// order.
func TestListImageStatuses_NameForSeveralTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names []string
	}{
		{name: "latest listed first", names: []string{"nginx:latest", "nginx:1.27"}},
		{name: "version listed first", names: []string{"nginx:1.27", "nginx:latest"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			containers := make([]*container.Container, 0, len(tt.names))

			for _, name := range tt.names {
				containers = append(containers, imageContainer(name, "sha256:abc123"))
			}

			client := mockImages.NewMockContainerLister(t)
			client.EXPECT().ListContainers(mock.Anything).Return(containers, nil)

			statuses, err := ListImageStatuses(t.Context(), client, nil)
			require.NoError(t, err)
			require.Len(t, statuses, 1)
			assert.Equal(t, "nginx:1.27", statuses[0].Name)
			assert.Equal(t, 2, statuses[0].Containers)
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
			assert.NotNil(t, result, "an empty result encodes as a JSON array, not null")
			assert.Len(t, result, tt.wantLen)
		})
	}
}
