// Package helpers provides tests for utility functions related to registry operations in Watchtower.
// It verifies the behavior of registry address extraction and digest normalization.
package helpers

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestHelpers(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Helper Suite")
}

var _ = ginkgo.Describe("the helpers", func() {
	ginkgo.Describe("GetRegistryAddress", func() {
		ginkgo.It("should return error if passed empty string", func() {
			_, err := GetRegistryAddress("")
			gomega.Expect(err).To(gomega.HaveOccurred())
		})
		ginkgo.It("should return index.docker.io for image refs with no explicit registry", func() {
			gomega.Expect(GetRegistryAddress("watchtower")).To(gomega.Equal("index.docker.io"))
			gomega.Expect(GetRegistryAddress("nickfedor/watchtower")).To(gomega.Equal("index.docker.io"))
		})
		ginkgo.It("should return index.docker.io for image refs with docker.io domain", func() {
			gomega.Expect(GetRegistryAddress("docker.io/watchtower")).To(gomega.Equal("index.docker.io"))
			gomega.Expect(GetRegistryAddress("docker.io/nickfedor/watchtower")).To(gomega.Equal("index.docker.io"))
		})
		ginkgo.It("should return the host if passed an image name containing a local host", func() {
			gomega.Expect(GetRegistryAddress("henk:80/watchtower")).To(gomega.Equal("henk:80"))
			gomega.Expect(GetRegistryAddress("localhost/watchtower")).To(gomega.Equal("localhost"))
		})
		ginkgo.It("should return the server address if passed a fully qualified image name", func() {
			gomega.Expect(GetRegistryAddress("github.com/nicholas-fedor/config")).To(gomega.Equal("github.com"))
		})
	})

	ginkgo.Describe("NormalizeDigest", func() {
		ginkgo.It("should trim sha256: prefix from digest", func() {
			input := "sha256:d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
			expected := "d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
			gomega.Expect(NormalizeDigest(input)).To(gomega.Equal(expected))
		})

		ginkgo.It("should return unchanged digest without recognized prefix", func() {
			input := "d68e1e532088964195ad3a0a71526bc2f11a78de0def85629beb75e2265f0547"
			gomega.Expect(NormalizeDigest(input)).To(gomega.Equal(input))
		})

		ginkgo.It("should handle empty digest string", func() {
			input := ""
			gomega.Expect(NormalizeDigest(input)).To(gomega.Equal(""))
		})

		ginkgo.It("should handle digest with unrecognized prefix", func() {
			input := "md5:1234567890abcdef"
			gomega.Expect(NormalizeDigest(input)).To(gomega.Equal(input))
		})
	})
})
