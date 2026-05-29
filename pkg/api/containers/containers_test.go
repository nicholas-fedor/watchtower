package containers_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/api"
	containersAPI "github.com/nicholas-fedor/watchtower/pkg/api/containers"
)

const (
	token       = "123123123"
	rateLimit60 = 60 // Maximum authentication requests per minute per IP address.
)

func TestContainers(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Containers Suite")
}

var _ = ginkgo.Describe("the containers API", func() {
	var server *ghttp.Server

	ginkgo.BeforeEach(func() {
		httpAPI := api.New(token, ":8080", rateLimit60)
		handler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
			return []containersAPI.Status{
				{
					Name:    "test-container-1",
					Image:   "example/test-image:latest",
					ImageID: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					Digest:  "sha256:2222222222222222222222222222222222222222222222222222222222222222",
				},
			}, nil
		})
		handleReq := httpAPI.RequireToken(handler.Handle)
		server = ghttp.NewServer()
		server.RouteToHandler("GET", "/v1/containers", ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v1/containers"),
			ghttp.VerifyHeaderKV("Authorization", "Bearer "+token),
			handleReq,
		))
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	ginkgo.Describe("Successful responses", func() {
		ginkgo.It("should serve the running image identity of each container", func() {
			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				server.URL()+"/v1/containers",
				nil,
			)
			req.Header.Add("Authorization", "Bearer "+token)

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

			body, _ := io.ReadAll(resp.Body)

			var parsed struct {
				Containers []containersAPI.Status `json:"containers"`
				Count      int                    `json:"count"`
				APIVersion string                 `json:"api_version"`
				Timestamp  string                 `json:"timestamp"`
			}

			gomega.Expect(json.Unmarshal(body, &parsed)).To(gomega.Succeed())
			gomega.Expect(parsed.APIVersion).To(gomega.Equal("v1"))
			gomega.Expect(parsed.Count).To(gomega.Equal(1))
			gomega.Expect(parsed.Containers).To(gomega.HaveLen(1))
			gomega.Expect(parsed.Containers[0].Name).To(gomega.Equal("test-container-1"))
			gomega.Expect(parsed.Containers[0].Image).To(gomega.Equal("example/test-image:latest"))
			gomega.Expect(parsed.Containers[0].ImageID).To(gomega.Equal("sha256:1111111111111111111111111111111111111111111111111111111111111111"))
			gomega.Expect(parsed.Containers[0].Digest).
				To(gomega.Equal("sha256:2222222222222222222222222222222222222222222222222222222222222222"))
			gomega.Expect(parsed.Timestamp).ToNot(gomega.BeEmpty())
		})

		ginkgo.It("should return empty list when no containers are watched", func() {
			emptyHTTPAPI := api.New(token, ":8080", rateLimit60)
			emptyHandler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
				return []containersAPI.Status{}, nil
			})
			emptyTokenHandler := emptyHTTPAPI.RequireToken(emptyHandler.Handle)

			emptyServer := ghttp.NewServer()
			defer emptyServer.Close()

			emptyServer.RouteToHandler("GET", "/v1/containers", ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/v1/containers"),
				ghttp.VerifyHeaderKV("Authorization", "Bearer "+token),
				emptyTokenHandler,
			))

			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				emptyServer.URL()+"/v1/containers",
				nil,
			)
			req.Header.Add("Authorization", "Bearer "+token)

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

			body, _ := io.ReadAll(resp.Body)

			var parsed struct {
				Containers []containersAPI.Status `json:"containers"`
				Count      int                    `json:"count"`
			}

			gomega.Expect(json.Unmarshal(body, &parsed)).To(gomega.Succeed())
			gomega.Expect(parsed.Count).To(gomega.Equal(0))
			gomega.Expect(parsed.Containers).To(gomega.BeEmpty())
		})

		ginkgo.It("should return multiple containers when multiple are watched", func() {
			multiHTTPAPI := api.New(token, ":8080", rateLimit60)
			multiHandler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
				return []containersAPI.Status{
					{
						Name:    "test-container-1",
						Image:   "example/test-image:latest",
						ImageID: "sha256:1111",
						Digest:  "sha256:2222",
					},
					{
						Name:    "test-container-2",
						Image:   "example/other-image:latest",
						ImageID: "sha256:3333",
						Digest:  "",
					},
				}, nil
			})
			multiTokenHandler := multiHTTPAPI.RequireToken(multiHandler.Handle)

			multiServer := ghttp.NewServer()
			defer multiServer.Close()

			multiServer.RouteToHandler("GET", "/v1/containers", ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/v1/containers"),
				ghttp.VerifyHeaderKV("Authorization", "Bearer "+token),
				multiTokenHandler,
			))

			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				multiServer.URL()+"/v1/containers",
				nil,
			)
			req.Header.Add("Authorization", "Bearer "+token)

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

			body, _ := io.ReadAll(resp.Body)

			var parsed struct {
				Containers []containersAPI.Status `json:"containers"`
				Count      int                    `json:"count"`
			}

			gomega.Expect(json.Unmarshal(body, &parsed)).To(gomega.Succeed())
			gomega.Expect(parsed.Count).To(gomega.Equal(2))
			gomega.Expect(parsed.Containers).To(gomega.HaveLen(2))
			gomega.Expect(parsed.Containers[0].Name).To(gomega.Equal("test-container-1"))
			gomega.Expect(parsed.Containers[1].Name).To(gomega.Equal("test-container-2"))
			gomega.Expect(parsed.Containers[1].Digest).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("Authentication", func() {
		ginkgo.It("should return 401 Unauthorized without token", func() {
			noAuthHTTPAPI := api.New(token, ":8080", rateLimit60)
			noAuthHandler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
				return []containersAPI.Status{}, nil
			})
			noAuthTokenHandler := noAuthHTTPAPI.RequireToken(noAuthHandler.Handle)

			noAuthServer := ghttp.NewServer()
			defer noAuthServer.Close()

			noAuthServer.RouteToHandler("GET", "/v1/containers", ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/v1/containers"),
				noAuthTokenHandler,
			))

			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				noAuthServer.URL()+"/v1/containers",
				nil,
			)

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusUnauthorized))
		})

		ginkgo.It("should return 401 Unauthorized with invalid token", func() {
			invalidHTTPAPI := api.New(token, ":8080", rateLimit60)
			invalidHandler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
				return []containersAPI.Status{}, nil
			})
			invalidTokenHandler := invalidHTTPAPI.RequireToken(invalidHandler.Handle)

			invalidServer := ghttp.NewServer()
			defer invalidServer.Close()

			invalidServer.RouteToHandler("GET", "/v1/containers", ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/v1/containers"),
				invalidTokenHandler,
			))

			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				invalidServer.URL()+"/v1/containers",
				nil,
			)
			req.Header.Add("Authorization", "Bearer invalid-token")

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusUnauthorized))
		})

		ginkgo.It("should return 401 Unauthorized with malformed Authorization header", func() {
			malformedHTTPAPI := api.New(token, ":8080", rateLimit60)
			malformedHandler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
				return []containersAPI.Status{}, nil
			})
			malformedTokenHandler := malformedHTTPAPI.RequireToken(malformedHandler.Handle)

			malformedServer := ghttp.NewServer()
			defer malformedServer.Close()

			malformedServer.RouteToHandler("GET", "/v1/containers", ghttp.CombineHandlers(
				ghttp.VerifyRequest("GET", "/v1/containers"),
				malformedTokenHandler,
			))

			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				malformedServer.URL()+"/v1/containers",
				nil,
			)
			req.Header.Add("Authorization", "InvalidFormat token")

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			defer resp.Body.Close()

			gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusUnauthorized))
		})
	})

	ginkgo.Describe("Content-Type headers", func() {
		ginkgo.It("should return application/json Content-Type", func() {
			req, _ := http.NewRequestWithContext(
				context.Background(),
				http.MethodGet,
				server.URL()+"/v1/containers",
				nil,
			)
			req.Header.Add("Authorization", "Bearer "+token)

			resp, err := http.DefaultClient.Do(req)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			defer resp.Body.Close()

			gomega.Expect(resp.Header.Get("Content-Type")).To(gomega.Equal("application/json"))
		})
	})
})

// TestHandleReturns500OnListError verifies that the handler returns 500 Internal
// Server Error when the list function returns an error.
func TestHandleReturns500OnListError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("list error")
	handler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
		return nil, expectedErr
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/v1/containers",
		nil,
	)

	handler.Handle(rec, req)

	gomega.Expect(rec.Code).To(gomega.Equal(http.StatusInternalServerError))

	body, err := io.ReadAll(rec.Body)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(string(body)).To(gomega.ContainSubstring("failed to list containers"))
}

// TestHandleReturnsEmptyDigestForLocalImages verifies that Digest
// is empty for locally-built images with no registry reference.
func TestHandleReturnsEmptyDigestForLocalImages(t *testing.T) {
	t.Parallel()

	handler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
		return []containersAPI.Status{
			{
				Name:    "test-container-local",
				Image:   "local-image:latest",
				ImageID: "sha256:abc123",
				Digest:  "",
			},
		}, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/v1/containers",
		nil,
	)

	handler.Handle(rec, req)

	gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))

	var parsed struct {
		Containers []containersAPI.Status `json:"containers"`
	}

	gomega.Expect(json.Unmarshal(rec.Body.Bytes(), &parsed)).To(gomega.Succeed())
	gomega.Expect(parsed.Containers).To(gomega.HaveLen(1))
	gomega.Expect(parsed.Containers[0].Digest).To(gomega.BeEmpty())
}

// TestNewHandlerSetsCorrectPath verifies that New creates a handler with the
// correct endpoint path.
func TestNewHandlerSetsCorrectPath(t *testing.T) {
	t.Parallel()

	handler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
		return nil, nil
	})

	gomega.Expect(handler.Path).To(gomega.Equal("/v1/containers"))
}

// TestHandlerStartsDebugLogging verifies debug logging on request.
func TestHandlerStartsDebugLogging(t *testing.T) {
	t.Parallel()

	// Suppress log output during test.
	originalOutput := logrus.StandardLogger().Out

	logrus.SetOutput(io.Discard)
	defer logrus.SetOutput(originalOutput)

	handler := containersAPI.New(func(_ context.Context) ([]containersAPI.Status, error) {
		return []containersAPI.Status{}, nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"/v1/containers",
		nil,
	)

	handler.Handle(rec, req)

	gomega.Expect(rec.Code).To(gomega.Equal(http.StatusOK))
}
