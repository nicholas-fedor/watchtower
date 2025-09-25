package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func getWithToken(handler http.Handler) map[string]string {
	metricMap := map[string]string{}
	respWriter := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodGet, getURL, nil)
	req.Header.Add("Authorization", "Bearer "+token)

	handler.ServeHTTP(respWriter, req)

	res := respWriter.Result()
	body, _ := io.ReadAll(res.Body)

	for line := range strings.SplitSeq(string(body), "\n") {
		if len(line) < 1 || line[0] == '#' {
			continue
		}

		parts := strings.Split(line, " ")
		metricMap[parts[0]] = parts[1]
	}

	return metricMap
}

var _ = ginkgo.Describe("the metrics API", func() {
	httpAPI := api.New(token, ":8080")
	m := metricsAPI.New()

	handleReq := httpAPI.RequireToken(m.Handle)
	tryGetMetrics := func() map[string]string { return getWithToken(handleReq) }

	ginkgo.It("should serve metrics", func() {
		gomega.Expect(tryGetMetrics()).
			To(gomega.HaveKeyWithValue("watchtower_containers_updated", "0"))

		metric := &metrics.Metric{
			Scanned: 4,
			Updated: 3,
			Failed:  1,
		}

		metrics.Default().RegisterScan(metric)
		gomega.Eventually(metrics.Default().QueueIsEmpty).Should(gomega.BeTrue())

		gomega.Eventually(tryGetMetrics).Should(gomega.SatisfyAll(
			gomega.HaveKeyWithValue("watchtower_containers_updated", "3"),
			gomega.HaveKeyWithValue("watchtower_containers_failed", "1"),
			gomega.HaveKeyWithValue("watchtower_containers_scanned", "4"),
			gomega.HaveKeyWithValue("watchtower_scans_total", "1"),
			gomega.HaveKeyWithValue("watchtower_scans_skipped_total", "0"),
		))

		for range 3 {
			metrics.Default().RegisterScan(nil)
		}
		gomega.Eventually(metrics.Default().QueueIsEmpty).Should(gomega.BeTrue())

		gomega.Eventually(tryGetMetrics).Should(gomega.SatisfyAll(
			gomega.HaveKeyWithValue("watchtower_scans_total", "4"),
			gomega.HaveKeyWithValue("watchtower_scans_skipped_total", "3"),
		))
	})
})
