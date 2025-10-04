// Package manifest_test provides tests for constructing manifest URLs in Watchtower.
// It verifies the behavior of BuildManifestURL across various image reference scenarios.
package manifest_test

import (
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/spf13/viper"

	dockerImageType "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/registry/manifest"
)

func TestManifest(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Manifest Suite")
}

var _ = ginkgo.BeforeEach(func() {
	// Ensure WATCHTOWER_REGISTRY_TLS_SKIP is disabled by default for HTTPS tests.
	viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)
})

var _ = ginkgo.Describe("the manifest module", func() {
	ginkgo.Describe("BuildManifestURL", func() {
		ginkgo.It("should return a valid url given a fully qualified image", func() {
			imageRef := "ghcr.io/nicholas-fedor/watchtower:mytag"
			expected := "https://ghcr.io/v2/nicholas-fedor/watchtower/manifests/mytag"

			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.Equal(expected))
		})

		ginkgo.It("should assume Docker Hub for image refs with no explicit registry", func() {
			imageRef := "nickfedor/watchtower:latest"
			expected := "https://index.docker.io/v2/nickfedor/watchtower/manifests/latest"

			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.Equal(expected))
		})

		ginkgo.It("should assume latest for image refs with no explicit tag", func() {
			imageRef := "nickfedor/watchtower"
			expected := "https://index.docker.io/v2/nickfedor/watchtower/manifests/latest"

			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.Equal(expected))
		})

		ginkgo.It(
			"should not prepend library/ for single-part container names in registries other than Docker Hub",
			func() {
				imageRef := "docker-registry.domain/imagename:latest"
				expected := "https://docker-registry.domain/v2/imagename/manifests/latest"

				URL, err := buildMockContainerManifestURL(imageRef)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(URL).To(gomega.Equal(expected))
			},
		)

		ginkgo.It("should throw an error on pinned images with digest", func() {
			imageRef := "docker-registry.domain/imagename@sha256:daf7034c5c89775afe3008393ae033529913548243b84926931d7c84398ecda7"
			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).
				To(gomega.ContainSubstring("parsed container image reference has no tag"))
			gomega.Expect(err.Error()).To(gomega.ContainSubstring(imageRef))
			gomega.Expect(URL).To(gomega.BeEmpty())
		})

		ginkgo.It("should return an error for invalid image references", func() {
			imageRef := "invalid:image:ref:!!"
			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to parse image name"))
			gomega.Expect(URL).To(gomega.BeEmpty())
		})

		ginkgo.It("should prepend library/ for Docker Hub official images", func() {
			imageRef := "nginx:latest"
			expected := "https://index.docker.io/v2/library/nginx/manifests/latest"

			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.Equal(expected))
		})

		ginkgo.It("should handle hosts with ports correctly", func() {
			imageRef := "localhost:5000/repo/image:tag"
			expected := "https://localhost:5000/v2/repo/image/manifests/tag"

			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.Equal(expected))
		})

		ginkgo.It("should use HTTP scheme when WATCHTOWER_REGISTRY_TLS_SKIP is enabled", func() {
			viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", true)
			defer viper.Set("WATCHTOWER_REGISTRY_TLS_SKIP", false)

			imageRef := "ghcr.io/nicholas-fedor/watchtower:mytag"
			expected := "http://ghcr.io/v2/nicholas-fedor/watchtower/manifests/mytag"

			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.Equal(expected))
		})
	})
})

// buildMockContainerManifestURL creates a mock container and builds its manifest URL.
// It constructs a container with the given image reference for testing BuildManifestURL.
func buildMockContainerManifestURL(imageRef string) (string, error) {
	imageInfo := dockerImageType.InspectResponse{
		RepoTags: []string{
			imageRef,
		},
	}
	mockID := "mock-id"
	mockName := "mock-container"
	mockCreated := time.Now()
	mock := mocks.CreateMockContainerWithImageInfo(
		mockID,
		mockName,
		imageRef,
		mockCreated,
		imageInfo,
	)

	// Determine scheme based on WATCHTOWER_REGISTRY_TLS_SKIP.
	scheme := "https"
	if viper.GetBool("WATCHTOWER_REGISTRY_TLS_SKIP") {
		scheme = "http"
	}

	return manifest.BuildManifestURL(mock, scheme)
}
