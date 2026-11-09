package provenance

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
)

func TestLastStamp(t *testing.T) {
	t.Parallel()

	t.Run("nil container", func(t *testing.T) {
		t.Parallel()

		commit, tag := LastStamp(nil)
		assert.Empty(t, commit)
		assert.Empty(t, tag)
	})

	t.Run("reads labels", func(t *testing.T) {
		t.Parallel()

		commit, tag := LastStamp(testContainer(map[string]string{
			labels.GitLastCommit: "  abc123  ",
			labels.GitLastTag:    "v1.2.3",
		}, "app:latest"))
		assert.Equal(t, "abc123", commit)
		assert.Equal(t, "v1.2.3", tag)
	})

	t.Run("missing labels", func(t *testing.T) {
		t.Parallel()

		commit, tag := LastStamp(testContainer(nil, "app:latest"))
		assert.Empty(t, commit)
		assert.Empty(t, tag)
	})
}

func TestBaseline(t *testing.T) {
	t.Parallel()

	t.Run("stamp wins over image", func(t *testing.T) {
		t.Parallel()

		commit, tag := Baseline(testContainer(map[string]string{
			labels.GitLastCommit: "stampsha",
			labels.GitLastTag:    "v1.0.0",
		}, "app:git-aaa111bbbbcc"))
		assert.Equal(t, "stampsha", commit)
		assert.Equal(t, "v1.0.0", tag)
	})

	t.Run("image revision when no stamp", func(t *testing.T) {
		t.Parallel()

		commit, tag := Baseline(imageContainer("app:latest", "oci123", "v1.2.3"))
		assert.Equal(t, "oci123", commit)
		assert.Equal(t, "v1.2.3", tag)
	})

	t.Run("empty when unknown", func(t *testing.T) {
		t.Parallel()

		commit, tag := Baseline(imageContainer("app:latest", "", ""))
		assert.Empty(t, commit)
		assert.Empty(t, tag)
	})
}

func TestImageRevision(t *testing.T) {
	t.Parallel()

	t.Run("nil container", func(t *testing.T) {
		t.Parallel()

		commit, tag := imageRevision(nil)
		assert.Empty(t, commit)
		assert.Empty(t, tag)
	})

	t.Run("git shortsha image name", func(t *testing.T) {
		t.Parallel()

		commit, tag := imageRevision(imageContainer("localhost:5000/app:git-aaa111bbbbcc", "", ""))
		assert.Equal(t, "aaa111bbbbcc", commit)
		assert.Empty(t, tag)
	})

	t.Run("git shortsha in another tag of the image", func(t *testing.T) {
		t.Parallel()

		c := modelfixture.Container(
			containerfixture.WithImageName("app:latest"),
			containerfixture.WithRepoTags([]string{"app:latest", "app:git-aaa111bbbbcc"}),
		)

		commit, tag := imageRevision(c)
		assert.Equal(t, "aaa111bbbbcc", commit)
		assert.Empty(t, tag)
	})

	t.Run("ignores non git tags", func(t *testing.T) {
		t.Parallel()

		commit, tag := imageRevision(imageContainer("app:latest", "", ""))
		assert.Empty(t, commit)
		assert.Empty(t, tag)
	})
}

func TestGitSHAFromRef(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "aaa111bbbbcc", gitSHAFromRef("app:git-aaa111bbbbcc"))
	assert.Equal(t, "aaa111bbbbcc", gitSHAFromRef("localhost:5000/app:git-aaa111bbbbcc"))
	assert.Empty(t, gitSHAFromRef("app:latest"))
	assert.Empty(t, gitSHAFromRef("app:git-short"))
	assert.Empty(t, gitSHAFromRef("app@sha256:deadbeef"))
	assert.Empty(t, gitSHAFromRef(""))
}

// imageContainer returns a container with an image name and optional OCI
// annotations. Without annotations its image info is missing, as for an image
// that is no longer present.
//
// Parameters:
//   - imageName: Image reference the container runs.
//   - revision: OCI revision label, or empty.
//   - version: OCI version label, or empty.
//
// Returns:
//   - *container.Container: Container built from inspect data.
func imageContainer(imageName, revision, version string) *container.Container {
	if revision == "" && version == "" {
		return modelfixture.ContainerWithoutImageInfo(containerfixture.WithImageName(imageName))
	}

	imageLabels := map[string]string{}
	if revision != "" {
		imageLabels[oci.RevisionLabel] = revision
	}

	if version != "" {
		imageLabels[oci.VersionLabel] = version
	}

	return modelfixture.Container(
		containerfixture.WithImageName(imageName),
		containerfixture.WithImageLabels(imageLabels),
	)
}
