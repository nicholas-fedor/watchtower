package container

import (
	"context"
	"errors"
	"net/http"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/docker/docker/api/types/container"
	dockerImage "github.com/docker/docker/api/types/image"
	dockerClient "github.com/docker/docker/client"
	gomegaTypes "github.com/onsi/gomega/types"

	"github.com/nicholas-fedor/watchtower/internal/util"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("the client", func() {
	var (
		docker     *dockerClient.Client
		mockServer *ghttp.Server
	)

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
			c := &client{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnAlways}}

			ginkgo.It("should always return true", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerUnknown)).To(gomega.BeTrue())
				gomega.Expect(c.WarnOnHeadPullFailed(containerKnown)).To(gomega.BeTrue())
			})
		})
		ginkgo.When(`warn on head failure is set to "auto"`, func() {
			c := &client{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnAuto}}

			ginkgo.It("should return false for unknown repos", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerUnknown)).To(gomega.BeFalse())
			})
			ginkgo.It("should return true for known repos", func() {
				gomega.Expect(c.WarnOnHeadPullFailed(containerKnown)).To(gomega.BeTrue())
			})
		})
		ginkgo.When(`warn on head failure is set to "never"`, func() {
			c := &client{ClientOptions: ClientOptions{WarnOnHeadFailed: WarnNever}}

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
	ginkgo.When("pulling an image that requires authentication", func() {
		ginkgo.It("should log at Warn level and return errPullImageUnauthorized for auth failures", func() {
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp("/images/")),
					ghttp.RespondWith(http.StatusOK, `{"Id":"sha256:abc","RepoDigests":["private-registry.io/app@sha256:abc"]}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", gomega.MatchRegexp("/images/create")),
					ghttp.RespondWith(http.StatusUnauthorized, `{"message":"unauthorized: authentication required"}`),
				),
			)

			i := newImageClient(docker)
			pullContainer := MockContainer(WithImageName("private-registry.io/app:latest"))

			resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			err := i.PullImage(context.Background(), pullContainer, WarnAuto)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("authentication required"))
			gomega.Expect(errors.Is(err, errPullImageUnauthorized)).To(gomega.BeTrue())
			gomega.Expect(errors.Is(err, errPullImageFailed)).To(gomega.BeFalse())
			gomega.Eventually(logbuf).Should(gbytes.Say(`level=warning`))
			gomega.Eventually(logbuf).Should(gbytes.Say(`Image pull failed: authentication required`))
		})
	})
	ginkgo.When("pulling an image that does not exist in registry", func() {
		ginkgo.It("should log at Debug level and return errPullImageNotFound for not found errors", func() {
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp("/images/")),
					ghttp.RespondWith(http.StatusOK, `{"Id":"sha256:def","RepoDigests":["nonexistent@sha256:def"]}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", gomega.MatchRegexp("/images/create")),
					ghttp.RespondWith(http.StatusNotFound, `{"message":"manifest for nonexistent:latest not found"}`),
				),
			)

			i := newImageClient(docker)
			pullContainer := MockContainer(WithImageName("nonexistent:latest"))

			resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			err := i.PullImage(context.Background(), pullContainer, WarnAuto)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("image not found"))
			gomega.Expect(errors.Is(err, errPullImageNotFound)).To(gomega.BeTrue())
			gomega.Expect(errors.Is(err, errPullImageFailed)).To(gomega.BeFalse())
			gomega.Eventually(logbuf).Should(gbytes.Say(`Image pull failed: image not found in registry`))
		})
	})
	ginkgo.When("pulling an image with a server error", func() {
		ginkgo.It("should log at Debug level and return errPullImageFailed for other errors", func() {
			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp("/images/")),
					ghttp.RespondWith(http.StatusOK, `{"Id":"sha256:ghi","RepoDigests":["app@sha256:ghi"]}`),
				),
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("POST", gomega.MatchRegexp("/images/create")),
					ghttp.RespondWith(http.StatusInternalServerError, `{"message":"internal server error"}`),
				),
			)

			i := newImageClient(docker)
			pullContainer := MockContainer(WithImageName("app:latest"))

			resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			err := i.PullImage(context.Background(), pullContainer, WarnAuto)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to pull image"))
			gomega.Expect(errors.Is(err, errPullImageFailed)).To(gomega.BeTrue())
			gomega.Expect(errors.Is(err, errPullImageUnauthorized)).To(gomega.BeFalse())
			gomega.Expect(errors.Is(err, errPullImageNotFound)).To(gomega.BeFalse())
			gomega.Eventually(logbuf).Should(gbytes.Say(`Failed to initiate image pull`))
		})
	})
	ginkgo.When("removing a image", func() {
		ginkgo.When("debug logging is enabled", func() {
			ginkgo.It("should log removed and untagged images", func() {
				imageA := util.GenerateRandomSHA256()
				imageAParent := util.GenerateRandomSHA256()
				images := map[string][]string{imageA: {imageAParent}}
				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.MatchRegexp("/containers/json")),
						ghttp.RespondWithJSONEncoded(http.StatusOK, []dockerContainer.Summary{}),
					),
					mockContainer.RemoveImageHandler(images),
				)

				c := &client{api: docker}

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				gomega.Expect(c.RemoveImageByID(context.Background(), types.ImageID(imageA), "test-image")).
					To(gomega.Succeed())
				shortA := types.ImageID(imageA).ShortID()
				shortAParent := types.ImageID(imageAParent).ShortID()
				expectedDeleted := shortA + ", " + shortAParent
				gomega.Eventually(logbuf).
					Should(gbytes.Say(`msg="Image removal details" deleted="%s" image_id=%s image_name=%s untagged=%s`, expectedDeleted, shortA, "test-image", shortA))
				gomega.Eventually(logbuf).
					Should(gbytes.Say(`msg="Cleaned up old image" image_id=%v image_name=%v`, shortA, "test-image"))
			})
		})
		ginkgo.When("image is not found", func() {
			ginkgo.It("should return an error", func() {
				image := util.GenerateRandomSHA256()

				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.MatchRegexp("/containers/json")),
						ghttp.RespondWithJSONEncoded(http.StatusOK, []dockerContainer.Summary{}),
					),
					mockContainer.RemoveImageHandler(nil),
				)

				c := &client{api: docker}
				err := c.RemoveImageByID(context.Background(), types.ImageID(image), "test-image")
				gomega.Expect(cerrdefs.IsNotFound(err)).To(gomega.BeTrue())
			})
		})
		ginkgo.When("image is used by an active container", func() {
			// Test cases for all active container states.
			// These states are defined in pkg/container/image.go lines 216-217.
			ginkgo.DescribeTable("should skip removal and return ErrImageInUse for each active state",
				func(tc struct {
					names string
					state string
				},
				) {
					imageA := util.GenerateRandomSHA256()

					mockServer.AppendHandlers(
						ghttp.CombineHandlers(
							ghttp.VerifyRequest("GET", gomega.MatchRegexp("/containers/json")),
							ghttp.RespondWithJSONEncoded(http.StatusOK, []dockerContainer.Summary{
								{
									ImageID: imageA,
									State:   tc.state,
								},
							}),
						),
					)

					c := &client{api: docker}

					resetLogrus, _ := captureLogrus(logrus.InfoLevel)
					defer resetLogrus()

					err := c.RemoveImageByID(context.Background(), types.ImageID(imageA), "test-image")
					gomega.Expect(err).To(gomega.MatchError(ErrImageInUse))
				},
				ginkgo.Entry("running", struct {
					names string
					state string
				}{names: "running", state: "running"}),
				ginkgo.Entry("restarting", struct {
					names string
					state string
				}{names: "restarting", state: "restarting"}),
				ginkgo.Entry("paused", struct {
					names string
					state string
				}{names: "paused", state: "paused"}),
				ginkgo.Entry("created", struct {
					names string
					state string
				}{names: "created", state: "created"}),
			)
		})
		ginkgo.When("ContainerList API fails", func() {
			ginkgo.It("should return an error and not proceed with removal", func() {
				imageA := util.GenerateRandomSHA256()

				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest("GET", gomega.MatchRegexp("/containers/json")),
						ghttp.RespondWithJSONEncoded(
							http.StatusInternalServerError,
							map[string]string{"message": "Internal server error"},
						),
					),
				)

				c := &client{api: docker}

				resetLogrus, _ := captureLogrus(logrus.InfoLevel)
				defer resetLogrus()

				err := c.RemoveImageByID(context.Background(), types.ImageID(imageA), "test-image")
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("cannot verify image usage"))
			})
		})
	})
	ginkgo.When("the context is canceled", func() {
		ginkgo.Describe("IsContainerStale", func() {
			ginkgo.It("should return context.Canceled error", func() {
				currentImageID := "sha256:" + util.GenerateRandomSHA256()
				container := MockContainer(
					WithImageName("test-image:latest"),
					func(container *dockerContainer.InspectResponse, image *dockerImage.InspectResponse) {
						container.Image = currentImageID
						image.ID = currentImageID
					},
				)

				// Create a canceled context
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately

				c := &client{api: docker}

				_, _, err := c.IsContainerStale(
					ctx,
					container,
					types.UpdateParams{NoPull: true},
				)
				gomega.Expect(err).To(gomega.MatchError(context.Canceled))
			})
		})
		ginkgo.Describe("RemoveImageByID", func() {
			ginkgo.It("should return context.Canceled error", func() {
				imageID := util.GenerateRandomSHA256()

				// Create a canceled context
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately

				c := &client{api: docker}

				err := c.RemoveImageByID(ctx, types.ImageID(imageID), "test-image")
				gomega.Expect(err).To(gomega.MatchError(context.Canceled))
			})
		})
	})
	ginkgo.When("checking container staleness with no-pull", func() {
		ginkgo.When("no newer local image exists", func() {
			ginkgo.It("should return false and current image ID", func() {
				currentImageID := "sha256:" + util.GenerateRandomSHA256()
				container := MockContainer(
					WithImageName("test-image:latest"),
					func(container *dockerContainer.InspectResponse, image *dockerImage.InspectResponse) {
						container.Image = currentImageID
						image.ID = currentImageID
					},
				)

				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.HaveSuffix("/images/test-image:latest/json"),
						),
						ghttp.RespondWithJSONEncoded(http.StatusOK, dockerImage.InspectResponse{
							ID: currentImageID,
						}),
					),
				)

				c := &client{api: docker}

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				stale, latestID, err := c.IsContainerStale(
					context.Background(),
					container,
					types.UpdateParams{NoPull: true},
				)
				gomega.Expect(err).To(gomega.Succeed())
				gomega.Expect(stale).To(gomega.BeFalse())
				gomega.Expect(latestID).To(gomega.Equal(types.ImageID(currentImageID)))
				gomega.Eventually(logbuf).
					Should(gbytes.Say(`msg="Skipping image pull due to no-pull setting - checking local image only"`))
				gomega.Eventually(logbuf).Should(gbytes.Say(`msg="No new image found"`))
			})
		})
		ginkgo.When("a newer local image exists", func() {
			ginkgo.It("should return true and new image ID", func() {
				currentImageID := "sha256:" + util.GenerateRandomSHA256()
				newImageID := "sha256:" + util.GenerateRandomSHA256()
				container := MockContainer(
					WithImageName("test-image:latest"),
					func(container *dockerContainer.InspectResponse, image *dockerImage.InspectResponse) {
						container.Image = currentImageID
						image.ID = currentImageID
					},
				)

				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.HaveSuffix("/images/test-image:latest/json"),
						),
						ghttp.RespondWithJSONEncoded(http.StatusOK, dockerImage.InspectResponse{
							ID: newImageID,
						}),
					),
				)

				c := &client{api: docker}

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				stale, latestID, err := c.IsContainerStale(
					context.Background(),
					container,
					types.UpdateParams{NoPull: true},
				)
				gomega.Expect(err).To(gomega.Succeed())
				gomega.Expect(stale).To(gomega.BeTrue())
				gomega.Expect(latestID).To(gomega.Equal(types.ImageID(newImageID)))
				gomega.Eventually(logbuf).
					Should(gbytes.Say(`msg="Skipping image pull due to no-pull setting - checking local image only"`))
				gomega.Eventually(logbuf).Should(gbytes.Say(`msg="Found new image"`))
			})
		})
		ginkgo.When("image inspection fails", func() {
			ginkgo.It("should return an error", func() {
				currentImageID := "sha256:" + util.GenerateRandomSHA256()
				container := MockContainer(
					WithImageName("test-image:latest"),
					func(container *dockerContainer.InspectResponse, image *dockerImage.InspectResponse) {
						container.Image = currentImageID
						image.ID = currentImageID
					},
				)

				mockServer.AppendHandlers(
					ghttp.CombineHandlers(
						ghttp.VerifyRequest(
							"GET",
							gomega.HaveSuffix("/images/test-image:latest/json"),
						),
						ghttp.RespondWithJSONEncoded(
							http.StatusNotFound,
							map[string]string{"message": "No such image"},
						),
					),
				)

				c := &client{api: docker}

				resetLogrus, logbuf := captureLogrus(logrus.DebugLevel)
				defer resetLogrus()

				stale, latestID, err := c.IsContainerStale(
					context.Background(),
					container,
					types.UpdateParams{NoPull: true},
				)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(stale).To(gomega.BeFalse())
				gomega.Expect(latestID).To(gomega.Equal(types.ImageID(currentImageID)))
				gomega.Eventually(logbuf).
					Should(gbytes.Say(`msg="Skipping image pull due to no-pull setting - checking local image only"`))
				gomega.Eventually(logbuf).Should(gbytes.Say(`msg="Failed to inspect latest image"`))
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
