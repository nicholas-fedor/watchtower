package registry_test

import (
	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"time"
)

var _ = ginkgo.Describe("Registry", func() {
	ginkgo.Describe("WarnOnAPIConsumption", func() {
		ginkgo.When("Given a container with an image from ghcr.io", func() {
			ginkgo.It("should want to warn", func() {
				gomega.Expect(testContainerWithImage("ghcr.io/nicholas-fedor/watchtower")).To(gomega.BeTrue())
			})
		})
		ginkgo.When("Given a container with an image implicitly from dockerhub", func() {
			ginkgo.It("should want to warn", func() {
				gomega.Expect(testContainerWithImage("docker:latest")).To(gomega.BeTrue())
			})
		})
		ginkgo.When("Given a container with an image explicitly from dockerhub", func() {
			ginkgo.It("should want to warn", func() {
				gomega.Expect(testContainerWithImage("index.docker.io/docker:latest")).To(gomega.BeTrue())
				gomega.Expect(testContainerWithImage("docker.io/docker:latest")).To(gomega.BeTrue())
			})
		})
		ginkgo.When("Given a container with an image from some other registry", func() {
			ginkgo.It("should not want to warn", func() {
				gomega.Expect(testContainerWithImage("docker.fsf.org/docker:latest")).To(gomega.BeFalse())
				gomega.Expect(testContainerWithImage("altavista.com/docker:latest")).To(gomega.BeFalse())
				gomega.Expect(testContainerWithImage("gitlab.com/docker:latest")).To(gomega.BeFalse())
			})
		})
	})
})

func testContainerWithImage(imageName string) bool {
	container := mocks.CreateMockContainer("", "", imageName, time.Now())
	return registry.WarnOnAPIConsumption(container)
}
