package container

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	cerrdefs "github.com/containerd/errdefs"
	dockerClient "github.com/docker/docker/client"
	gomegaTypes "github.com/onsi/gomega/types"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("the client", func() {
	var docker *dockerClient.Client
	var mockServer *ghttp.Server
	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()
		docker, _ = dockerClient.NewClientWithOpts(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()))
	})
	ginkgo.AfterEach(func() {
		mockServer.Close()
	})
	ginkgo.Describe("WarnOnHeadPullFailed", func() {
		containerUnknown := MockContainer(WithImageName("unknown.repo/prefix/imagename:latest"))
		containerKnown := MockContainer(WithImageName("docker.io/prefix/imagename:latest"))
		ginkgo.When(`warn on head failure is set to "always"`, func() {
			c := client{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnAlways}}
			ginkgo.It("should always return true", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerUnknown)).To(gomega.BeTrue())
				gomega.Expect(c.WarnOnHeadPullFailed(containerKnown)).To(gomega.BeTrue())
			})
		})
		ginkgo.When(`warn on head failure is set to "auto"`, func() {
			c := client{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnAuto}}
			ginkgo.It("should return false for unknown repos", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerUnknown)).To(gomega.BeFalse())
			})
			ginkgo.It("should return true for known repos", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerKnown)).To(gomega.BeTrue())
			})
		})
		ginkgo.When(`warn on head failure is set to "never"`, func() {
			c := client{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnNever}}
			ginkgo.It("should never return true", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerUnknown)).To(gomega.BeFalse())
				gomega.Expect(c.WarnOnHeadPullFailed(containerKnown)).To(gomega.BeFalse())
			})
		})
	})
	ginkgo.When("pulling the latest image", func() {
		ginkgo.When("the image consist of a pinned hash", func() {
			ginkgo.It("should gracefully fail with a useful message", func() {
				i := newImageClient(docker)
				pinnedContainer := MockContainer(
					WithImageName(
						"sha256:fa5269854a5e615e51a72b17ad3fd1e01268f278a6684c8ed3c5f0cdce3f230b",
					),
				)
				err := i.PullImage(context.Background(), pinnedContainer, WarnAuto)
				gomega.Expect(err).
					To(gomega.MatchError(`image is pinned with sha256, skipping pull`))
			})
		})
	})
	ginkgo.When("removing a image", func() {
		ginkgo.When("debug logging is enabled", func() {
			ginkgo.It("should log removed and untagged images", func() {
				imageA := util.GenerateRandomSHA256()
				imageAParent := util.GenerateRandomSHA256()
				images := map[string][]string{imageA: {imageAParent}}
				mockServer.AppendHandlers(mocks.RemoveImageHandler(images))
				c := client{api: docker}
				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()
				gomega.Expect(c.RemoveImageByID(types.ImageID(imageA))).To(gomega.Succeed())
				shortA := types.ImageID(imageA).ShortID()
				shortAParent := types.ImageID(imageAParent).ShortID()
				gomega.Eventually(logbuf).
					Should(gbytes.Say(`deleted="%v, %v" image_id=%v untagged=%v`, shortA, shortAParent, shortA, shortA))
			})
		})
		ginkgo.When("image is not found", func() {
			ginkgo.It("should return an error", func() {
				image := util.GenerateRandomSHA256()
				mockServer.AppendHandlers(mocks.RemoveImageHandler(nil))
				c := client{api: docker}
				err := c.RemoveImageByID(types.ImageID(image))
				gomega.Expect(cerrdefs.IsNotFound(err)).To(gomega.BeTrue())
			})
		})
	})
})

// withContainerImageName creates a Gomega matcher for container image name.
//
// Parameters:
//   - matcher: Matcher for the image name.
//
// Returns:
//   - gomegaTypes.GomegaMatcher: Matcher for verifying image name.
func withContainerImageName(matcher gomegaTypes.GomegaMatcher) gomegaTypes.GomegaMatcher {
	return gomega.WithTransform(func(container types.Container) string {
		return container.ImageName()
	}, matcher)
}
