package host_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"

	"github.com/nicholas-fedor/watchtower/pkg/api"
	"github.com/nicholas-fedor/watchtower/pkg/api/host"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

const (
	token = "123123123"
)

func TestHost(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Host Suite")
}

var _ = ginkgo.Describe("the host API", func() {
	var server *ghttp.Server
	var httpAPI *api.API
	var h *host.Handler
	var handleReq http.Handler
	var mockClient *mockTypes.MockClient

	ginkgo.BeforeEach(func() {
		httpAPI = api.New(token, ":8080")
		mockClient = mockTypes.NewMockClient(ginkgo.GinkgoT())

		h = host.New(mockClient)
		handleReq = httpAPI.RequireToken(h.ServeHTTP)
		server = ghttp.NewServer()
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	makeRequest := func(endpoint string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			server.URL()+"/v1/metrics/host"+endpoint,
			nil,
		)
		req.Header.Add("Authorization", "Bearer "+token)

		return http.DefaultClient.Do(req)
	}

	ginkgo.It("should serve system info", func() {
		mockClient.EXPECT().GetInfo().Return(types.SystemInfo{
			Name: "test-docker",
			RegistryConfig: &types.RegistryConfig{
				Mirrors:               []string{"mirror1", "mirror2"},
				InsecureRegistryCIDRs: []string{"10.0.0.0/8", "172.16.0.0/12"},
			},
		}, nil)

		server.RouteToHandler("GET", "/v1/metrics/host/info", ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v1/metrics/host/info"),
			ghttp.VerifyHeaderKV("Authorization", "Bearer "+token),
			handleReq.ServeHTTP,
		))

		resp, err := makeRequest("/info")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		var info types.SystemInfo
		err = json.Unmarshal(body, &info)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(info.Name).To(gomega.Equal("test-docker"))
		gomega.Expect(info.RegistryConfig).NotTo(gomega.BeNil())
		gomega.Expect(info.RegistryConfig.Mirrors).To(gomega.Equal([]string{"mirror1", "mirror2"}))
		gomega.Expect(info.RegistryConfig.InsecureRegistryCIDRs).
			To(gomega.Equal([]string{"10.0.0.0/8", "172.16.0.0/12"}))
	})

	ginkgo.It("should serve version info", func() {
		mockClient.EXPECT().GetServerVersion().Return(types.VersionInfo{
			Version: "20.10.0",
		}, nil)

		server.RouteToHandler("GET", "/v1/metrics/host/version", ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v1/metrics/host/version"),
			ghttp.VerifyHeaderKV("Authorization", "Bearer "+token),
			handleReq.ServeHTTP,
		))

		resp, err := makeRequest("/version")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		var version types.VersionInfo
		err = json.Unmarshal(body, &version)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(version.Version).To(gomega.Equal("20.10.0"))
	})

	ginkgo.It("should serve disk usage", func() {
		mockClient.EXPECT().GetDiskUsage().Return(types.DiskUsage{
			LayersSize: 1024 * 1024 * 1024,
		}, nil)

		server.RouteToHandler("GET", "/v1/metrics/host/disk-usage", ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v1/metrics/host/disk-usage"),
			ghttp.VerifyHeaderKV("Authorization", "Bearer "+token),
			handleReq.ServeHTTP,
		))

		resp, err := makeRequest("/disk-usage")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusOK))

		body, err := io.ReadAll(resp.Body)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		var usage types.DiskUsage
		err = json.Unmarshal(body, &usage)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(usage.LayersSize).To(gomega.Equal(int64(1024 * 1024 * 1024)))
	})

	ginkgo.It("should return 404 for unknown endpoint", func() {
		server.RouteToHandler("GET", "/v1/metrics/host/unknown", ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v1/metrics/host/unknown"),
			ghttp.VerifyHeaderKV("Authorization", "Bearer "+token),
			handleReq.ServeHTTP,
		))

		resp, err := makeRequest("/unknown")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(resp.StatusCode).To(gomega.Equal(http.StatusNotFound))
	})
})
