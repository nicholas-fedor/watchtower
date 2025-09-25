package metrics_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/pkg/api"
	metricsAPI "github.com/nicholas-fedor/watchtower/pkg/api/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

const (
	token  = "123123123"
	getURL = "http://localhost:8080/v1/metrics"
)

func TestMetrics(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Metrics Suite")
}

func getWithToken(handler http.Handler) map[string]int {
	metricMap := map[string]int{}
	respWriter := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, getURL, nil)
	req.Header.Add("Authorization", "Bearer "+token)

	handler.ServeHTTP(respWriter, req)

	res := respWriter.Result()
	body, _ := io.ReadAll(res.Body)

	json.Unmarshal(body, &metricMap)

	return metricMap
}

var _ = ginkgo.Describe("the metrics API", func() {
	httpAPI := api.New(token, ":8080")
	m := metricsAPI.New()

	handleReq := httpAPI.RequireToken(m.Handle)
	tryGetMetrics := func() map[string]int { return getWithToken(handleReq) }

	ginkgo.It("should serve metrics", func() {
		gomega.Expect(tryGetMetrics()).
			To(gomega.SatisfyAll(
				gomega.HaveKeyWithValue("scanned", 0),
				gomega.HaveKeyWithValue("updated", 0),
				gomega.HaveKeyWithValue("failed", 0),
			))

		metric := &metrics.Metric{
			Scanned: 4,
			Updated: 3,
			Failed:  1,
		}

		metrics.Default().RegisterScan(metric)
		gomega.Eventually(metrics.Default().QueueIsEmpty).Should(gomega.BeTrue())

		gomega.Eventually(tryGetMetrics).Should(gomega.SatisfyAll(
			gomega.HaveKeyWithValue("scanned", 4),
			gomega.HaveKeyWithValue("updated", 3),
			gomega.HaveKeyWithValue("failed", 1),
		))
	})
})
