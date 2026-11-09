package oci

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

func TestRead(t *testing.T) {
	t.Parallel()

	t.Run("nil container", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, Annotations{}, Read(nil))
	})

	t.Run("no image info", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, Annotations{}, Read(modelfixture.ContainerWithoutImageInfo()))
	})

	t.Run("nil image config", func(t *testing.T) {
		t.Parallel()

		c := modelfixture.Container(func(_ *dockerContainer.InspectResponse, image *dockerImage.InspectResponse) {
			image.Config = nil
		})
		assert.Equal(t, Annotations{}, Read(c))
	})

	t.Run("nil labels", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, Annotations{}, Read(modelfixture.Container(containerfixture.WithImageLabels(nil))))
	})

	t.Run("reads and trims labels", func(t *testing.T) {
		t.Parallel()

		c := modelfixture.Container(containerfixture.WithImageLabels(map[string]string{
			SourceLabel:        "  https://github.com/org/app  ",
			URLLabel:           "https://example.com/image",
			DocumentationLabel: "https://example.com/docs",
			RevisionLabel:      "abc123",
			VersionLabel:       "v1.2.3",
		}))

		assert.Equal(t, Annotations{
			Source:        "https://github.com/org/app",
			URL:           "https://example.com/image",
			Documentation: "https://example.com/docs",
			Revision:      "abc123",
			Version:       "v1.2.3",
		}, Read(c))
	})
}

func TestFromLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		want   Annotations
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   Annotations{},
		},
		{
			name:   "empty labels",
			labels: map[string]string{},
			want:   Annotations{},
		},
		{
			name:   "trims surrounding whitespace",
			labels: map[string]string{VersionLabel: "\t1.2.3\n", SourceLabel: " https://example.com/r "},
			want:   Annotations{Version: "1.2.3", Source: "https://example.com/r"},
		},
		{
			name:   "whitespace only becomes empty",
			labels: map[string]string{RevisionLabel: "   "},
			want:   Annotations{},
		},
		{
			name:   "ignores non-oci labels",
			labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
			want:   Annotations{},
		},
		{
			name: "reads every annotation",
			labels: map[string]string{
				SourceLabel:        "https://github.com/org/app",
				URLLabel:           "https://example.com/image",
				DocumentationLabel: "https://example.com/docs",
				RevisionLabel:      "abc123",
				VersionLabel:       "1.2.3",
			},
			want: Annotations{
				Source:        "https://github.com/org/app",
				URL:           "https://example.com/image",
				Documentation: "https://example.com/docs",
				Revision:      "abc123",
				Version:       "1.2.3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, FromLabels(tt.labels))
		})
	}
}

// Read must delegate to FromLabels so the new-image read path shares one implementation.
func TestReadDelegatesToFromLabels(t *testing.T) {
	t.Parallel()

	labels := map[string]string{VersionLabel: "1.2.3"}

	assert.Equal(t, FromLabels(labels), Read(modelfixture.Container(containerfixture.WithImageLabels(labels))))
}
