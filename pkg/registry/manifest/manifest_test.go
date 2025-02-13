package manifest_test

import (
	"testing"
	"time"

	apiTypes "github.com/docker/docker/api/types"
	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/registry/manifest"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestManifest(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Manifest Suite")
}

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
		ginkgo.It("should not prepend library/ for single-part container names in registries other than Docker Hub", func() {
			imageRef := "docker-registry.domain/imagename:latest"
			expected := "https://docker-registry.domain/v2/imagename/manifests/latest"

			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.Equal(expected))
		})
		ginkgo.It("should throw an error on pinned images", func() {
			imageRef := "docker-registry.domain/imagename@sha256:daf7034c5c89775afe3008393ae033529913548243b84926931d7c84398ecda7"
			URL, err := buildMockContainerManifestURL(imageRef)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(URL).To(gomega.BeEmpty())
		})
	})
})

func buildMockContainerManifestURL(imageRef string) (string, error) {
	imageInfo := apiTypes.ImageInspect{
		RepoTags: []string{
			imageRef,
		},
	}
	mockID := "mock-id"
	mockName := "mock-container"
	mockCreated := time.Now()
	mock := mocks.CreateMockContainerWithImageInfo(mockID, mockName, imageRef, mockCreated, imageInfo)

	return manifest.BuildManifestURL(mock)
}
