package oci

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	dockerImage "github.com/moby/moby/api/types/image"

	typemocks "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

func TestRead(t *testing.T) {
	t.Parallel()

	t.Run("nil container", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, Annotations{}, Read(nil))
	})

	t.Run("no image info", func(t *testing.T) {
		t.Parallel()

		c := typemocks.NewMockContainer(t)
		c.EXPECT().HasImageInfo().Return(false)
		assert.Equal(t, Annotations{}, Read(c))
	})

	t.Run("nil image inspect", func(t *testing.T) {
		t.Parallel()

		c := typemocks.NewMockContainer(t)
		c.EXPECT().HasImageInfo().Return(true)
		c.EXPECT().ImageInfo().Return(nil)
		assert.Equal(t, Annotations{}, Read(c))
	})

	t.Run("nil image config", func(t *testing.T) {
		t.Parallel()

		c := typemocks.NewMockContainer(t)
		c.EXPECT().HasImageInfo().Return(true)
		c.EXPECT().ImageInfo().Return(&dockerImage.InspectResponse{})
		assert.Equal(t, Annotations{}, Read(c))
	})

	t.Run("nil labels", func(t *testing.T) {
		t.Parallel()

		c := typemocks.NewMockContainer(t)
		c.EXPECT().HasImageInfo().Return(true)
		c.EXPECT().ImageInfo().Return(&dockerImage.InspectResponse{
			Config: &dockerspec.DockerOCIImageConfig{},
		})
		assert.Equal(t, Annotations{}, Read(c))
	})

	t.Run("reads and trims labels", func(t *testing.T) {
		t.Parallel()

		c := typemocks.NewMockContainer(t)
		c.EXPECT().HasImageInfo().Return(true)

		cfg := &dockerspec.DockerOCIImageConfig{}
		cfg.Labels = map[string]string{
			SourceLabel:        "  https://github.com/org/app  ",
			URLLabel:           "https://example.com/image",
			DocumentationLabel: "https://example.com/docs",
			RevisionLabel:      "abc123",
			VersionLabel:       "v1.2.3",
		}
		c.EXPECT().ImageInfo().Return(&dockerImage.InspectResponse{Config: cfg})

		assert.Equal(t, Annotations{
			Source:        "https://github.com/org/app",
			URL:           "https://example.com/image",
			Documentation: "https://example.com/docs",
			Revision:      "abc123",
			Version:       "v1.2.3",
		}, Read(c))
	})
}

func TestLooksLikeTag(t *testing.T) {
	t.Parallel()

	assert.True(t, LooksLikeTag("v1.2.3"))
	assert.True(t, LooksLikeTag("1.2.3"))
	assert.True(t, LooksLikeTag("  1.2.3  "))
	assert.False(t, LooksLikeTag("nightly"))
	assert.False(t, LooksLikeTag(""))
	assert.False(t, LooksLikeTag("  "))
}
