package container

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("CooldownError", func() {
	ginkgo.Describe("Error()", func() {
		ginkgo.When("the wrapped error is non-nil", func() {
			ginkgo.It("returns the wrapped error message", func() {
				inner := errors.New("inner failure")
				e := &CooldownError{Age: "1h", Delay: "2h", err: inner}
				gomega.Expect(e.Error()).To(gomega.Equal("inner failure"))
			})
		})

		ginkgo.When("the wrapped error is nil", func() {
			ginkgo.It("returns the ErrImageCooldown message", func() {
				e := &CooldownError{Age: "1h", Delay: "2h", Remaining: "1h", Passed: false, err: nil}
				gomega.Expect(e.Error()).To(gomega.Equal(ErrImageCooldown.Error()))
			})
		})
	})

	ginkgo.Describe("Unwrap()", func() {
		ginkgo.It("returns the wrapped error when non-nil", func() {
			inner := errors.New("wrapped")
			e := &CooldownError{err: inner}
			gomega.Expect(e.Unwrap()).To(gomega.Equal(inner))
		})

		ginkgo.It("returns nil when no wrapped error", func() {
			e := &CooldownError{}
			gomega.Expect(e.Unwrap()).To(gomega.Succeed())
		})
	})

	ginkgo.Describe("Is()", func() {
		ginkgo.It("matches when wrapped error is ErrImageCooldown", func() {
			e := &CooldownError{err: ErrImageCooldown}
			gomega.Expect(e.Is(ErrImageCooldown)).To(gomega.BeTrue())
		})

		ginkgo.It("does not match ErrImageCooldown with unrelated wrapped error", func() {
			e := &CooldownError{err: errors.New("some other error")}
			gomega.Expect(e.Is(ErrImageCooldown)).To(gomega.BeFalse())
		})

		ginkgo.It("does not match an unrelated target", func() {
			unrelated := errors.New("unrelated")
			e := &CooldownError{err: errors.New("some other error")}
			gomega.Expect(e.Is(unrelated)).To(gomega.BeFalse())
		})
	})

	ginkgo.Describe("errors.Is sentinel extraction", func() {
		ginkgo.It("allows errors.Is(err, ErrImageCooldown) on *CooldownError", func() {
			err := &CooldownError{
				Age:       "2 hours",
				Delay:     "1 day",
				Remaining: "22 hours",
				err:       ErrImageCooldown,
			}
			gomega.Expect(errors.Is(err, ErrImageCooldown)).To(gomega.BeTrue())
		})

		ginkgo.It("allows errors.As to extract *CooldownError with all fields", func() {
			err := &CooldownError{
				Age:       "2 hours",
				Delay:     "1 day",
				Remaining: "22 hours",
				err:       ErrImageCooldown,
			}

			var cooldownErr *CooldownError
			gomega.Expect(errors.As(err, &cooldownErr)).To(gomega.BeTrue())
			gomega.Expect(cooldownErr.Age).To(gomega.Equal("2 hours"))
			gomega.Expect(cooldownErr.Delay).To(gomega.Equal("1 day"))
			gomega.Expect(cooldownErr.Remaining).To(gomega.Equal("22 hours"))
		})
	})
})

var _ = ginkgo.Describe("isOutsideCooldown", func() {
	var (
		mockClient *dockerClient.Client
		mockServer *ghttp.Server
	)

	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()

		var err error

		mockClient, err = dockerClient.New(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()),
		)
		gomega.Expect(err).To(gomega.Succeed())
		mockServer.AppendHandlers(APIVersionPingHandler())
	})

	ginkgo.AfterEach(func() {
		mockServer.Close()
	})

	ginkgo.When("no cooldown delay is configured", func() {
		ginkgo.It("returns true with no registry calls", func() {
			i := newImageClient(mockClient)
			c := MockContainer(WithImageName("test:latest"))

			outside, err := i.isOutsideCooldown(
				context.Background(), c, types.UpdateParams{},
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(outside).To(gomega.BeTrue())
		})
	})

	ginkgo.When("cooldown delay is zero", func() {
		ginkgo.It("returns true immediately", func() {
			i := newImageClient(mockClient)
			c := MockContainer(WithImageName("test:latest"))

			outside, err := i.isOutsideCooldown(
				context.Background(), c, types.UpdateParams{
					CooldownDelay: 0,
				},
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(outside).To(gomega.BeTrue())
		})
	})

	ginkgo.When("container is monitor-only", func() {
		ginkgo.It("returns true and bypasses the cooldown check", func() {
			i := newImageClient(mockClient)
			c := MockContainer(WithImageName("test:latest"))

			outside, err := i.isOutsideCooldown(
				context.Background(), c, types.UpdateParams{
					MonitorOnly:   true,
					CooldownDelay: 24 * time.Hour,
				},
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(outside).To(gomega.BeTrue())
		})
	})

	ginkgo.When("container has no-pull enabled", func() {
		ginkgo.It("returns true and bypasses the cooldown check", func() {
			i := newImageClient(mockClient)
			c := MockContainer(WithImageName("test:latest"))

			outside, err := i.isOutsideCooldown(
				context.Background(), c, types.UpdateParams{
					NoPull:        true,
					CooldownDelay: 24 * time.Hour,
				},
			)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(outside).To(gomega.BeTrue())
		})
	})

	ginkgo.When("GetPullOptions fails (registry auth unavailable)", func() {
		ginkgo.It("returns false with a CooldownError wrapping the pull options error", func() {
			resetLogrus, _ := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			i := newImageClient(mockClient)

			imageName := "localhost/unsupported-image-format:latest"
			c := MockContainer(WithImageName(imageName))

			outside, err := i.isOutsideCooldown(
				context.Background(), c, types.UpdateParams{
					CooldownDelay: 1 * time.Hour,
				},
			)
			gomega.Expect(outside).To(gomega.BeFalse())
			gomega.Expect(err).To(gomega.HaveOccurred())

			var cooldownErr *CooldownError
			gomega.Expect(errors.As(err, &cooldownErr)).To(gomega.BeTrue())
			gomega.Expect(cooldownErr.Delay).ToNot(gomega.BeEmpty())
		})
	})

	ginkgo.When("FetchImageCreationTime fails (registry manifest returns 404)", func() {
		ginkgo.It("returns false with a CooldownError reflecting the age fetch failure", func() {
			resetLogrus, _ := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp("/v2/")),
					ghttp.RespondWith(http.StatusNotFound, "not found"),
				),
			)

			i := newImageClient(mockClient)
			c := MockContainer(WithImageName("test:latest"))

			outside, err := i.isOutsideCooldown(
				context.Background(), c, types.UpdateParams{
					CooldownDelay: 1 * time.Hour,
				},
			)
			gomega.Expect(outside).To(gomega.BeFalse())
			gomega.Expect(err).To(gomega.HaveOccurred())

			var cooldownErr *CooldownError
			gomega.Expect(errors.As(err, &cooldownErr)).To(gomega.BeTrue())
		})
	})
})

var _ = ginkgo.Describe("PullImage cooldown gate", func() {
	var (
		mockClient *dockerClient.Client
		mockServer *ghttp.Server
	)

	ginkgo.BeforeEach(func() {
		mockServer = ghttp.NewServer()

		var err error

		mockClient, err = dockerClient.New(
			dockerClient.WithHost(mockServer.URL()),
			dockerClient.WithHTTPClient(mockServer.HTTPTestServer.Client()),
		)
		gomega.Expect(err).To(gomega.Succeed())
		mockServer.AppendHandlers(APIVersionPingHandler())
		mockServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", gomega.MatchRegexp(`/info$`)),
				ghttp.RespondWithJSONEncoded(http.StatusOK, map[string]any{}),
			),
		)
	})

	ginkgo.AfterEach(func() {
		mockServer.Close()
	})

	ginkgo.When("image is stale and cooldown age fetch fails", func() {
		ginkgo.It("returns a CooldownError without pulling layers", func() {
			resetLogrus, _ := captureLogrus(logrus.DebugLevel)
			defer resetLogrus()

			mockServer.AppendHandlers(
				ghttp.CombineHandlers(
					ghttp.VerifyRequest("GET", gomega.MatchRegexp("/v2/")),
					ghttp.RespondWith(http.StatusNotFound, "not found"),
				),
			)

			i := newImageClient(mockClient)
			// Use an explicit registry host so local-only 404 handling does not apply.
			// Digest failure must fall through to the cooldown gate.
			c := MockContainer(
				WithImageName("registry.example.com/test:latest"),
				WithRepoDigests([]string{"registry.example.com/test@sha256:12345"}),
			)

			err := i.PullImage(context.Background(), c, WarnAuto, types.UpdateParams{
				CooldownDelay: 1 * time.Hour,
			})
			gomega.Expect(err).To(gomega.HaveOccurred())

			var cooldownErr *CooldownError
			gomega.Expect(errors.As(err, &cooldownErr)).To(gomega.BeTrue())
		})
	})
})
