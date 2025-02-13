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
})
