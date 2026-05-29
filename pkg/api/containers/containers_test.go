package containers_test

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
					Name:          "beacon",
					Image:         "ethpandaops/lighthouse:latest",
					ImageID:       "sha256:1111111111111111111111111111111111111111111111111111111111111111",
					RunningDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
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
		}

		gomega.Expect(json.Unmarshal(body, &parsed)).To(gomega.Succeed())
		gomega.Expect(parsed.APIVersion).To(gomega.Equal("v1"))
		gomega.Expect(parsed.Count).To(gomega.Equal(1))
		gomega.Expect(parsed.Containers).To(gomega.HaveLen(1))
		gomega.Expect(parsed.Containers[0].Name).To(gomega.Equal("beacon"))
		gomega.Expect(parsed.Containers[0].Image).To(gomega.Equal("ethpandaops/lighthouse:latest"))
		gomega.Expect(parsed.Containers[0].RunningDigest).
			To(gomega.Equal("sha256:2222222222222222222222222222222222222222222222222222222222222222"))
	})
})
