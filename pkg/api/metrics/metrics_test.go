package metrics_test

import (
	"fmt"
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
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Metrics Suite")
}

func getWithToken(handler http.Handler) map[string]string {
	metricMap := map[string]string{}
	respWriter := httptest.NewRecorder()

	req := httptest.NewRequest("GET", getURL, nil)
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

	handler.ServeHTTP(respWriter, req)

	res := respWriter.Result()
	body, _ := io.ReadAll(res.Body)

	for _, line := range strings.Split(string(body), "\n") {
		if len(line) < 1 || line[0] == '#' {
			continue
		}
		parts := strings.Split(line, " ")
		metricMap[parts[0]] = parts[1]
	}

	return metricMap
}

var _ = ginkgo.Describe("the metrics API", func() {
	httpAPI := api.New(token)
	m := metricsAPI.New()

	handleReq := httpAPI.RequireToken(m.Handle)
	tryGetMetrics := func() map[string]string { return getWithToken(handleReq) }

	ginkgo.It("should serve metrics", func() {

		gomega.Expect(tryGetMetrics()).To(gomega.HaveKeyWithValue("watchtower_containers_updated", "0"))

		metric := &metrics.Metric{
			Scanned: 4,
			Updated: 3,
			Failed:  1,
		}

		metrics.RegisterScan(metric)
		gomega.Eventually(metrics.Default().QueueIsEmpty).Should(gomega.BeTrue())

		gomega.Eventually(tryGetMetrics).Should(gomega.SatisfyAll(
			gomega.HaveKeyWithValue("watchtower_containers_updated", "3"),
			gomega.HaveKeyWithValue("watchtower_containers_failed", "1"),
			gomega.HaveKeyWithValue("watchtower_containers_scanned", "4"),
			gomega.HaveKeyWithValue("watchtower_scans_total", "1"),
			gomega.HaveKeyWithValue("watchtower_scans_skipped", "0"),
		))

		for i := 0; i < 3; i++ {
			metrics.RegisterScan(nil)
		}
		gomega.Eventually(metrics.Default().QueueIsEmpty).Should(gomega.BeTrue())

		gomega.Eventually(tryGetMetrics).Should(gomega.SatisfyAll(
			gomega.HaveKeyWithValue("watchtower_scans_total", "4"),
			gomega.HaveKeyWithValue("watchtower_scans_skipped", "3"),
		))
	})
})
