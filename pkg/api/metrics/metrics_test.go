package metrics_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"

	"github.com/nicholas-fedor/watchtower/pkg/api"
	metricsAPI "github.com/nicholas-fedor/watchtower/pkg/api/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
)

const (
	token = "123123123"
)

func TestMetrics(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Metrics Suite")
}

func getWithToken(baseURL string) (map[string]string, error) {
	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		baseURL+"/v1/metrics",
		nil,
	)
	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	metricMap := map[string]string{}

	for line := range strings.SplitSeq(string(body), "\n") {
		if len(line) < 1 || line[0] == '#' {
			continue
		}

		parts := strings.Split(line, " ")
		metricMap[parts[0]] = parts[1]
	}

	return metricMap, nil
}

var _ = ginkgo.Describe("the metrics API", func() {
	var (
		server    *ghttp.Server
		httpAPI   *api.API
		m         *metricsAPI.Handler
		handleReq http.HandlerFunc
	)

	ginkgo.BeforeEach(func() {
		httpAPI = api.New(token, ":8080")
		m = metricsAPI.New()
		handleReq = httpAPI.RequireToken(m.Handle)
		server = ghttp.NewServer()
		server.RouteToHandler("GET", "/v1/metrics", ghttp.CombineHandlers(
			ghttp.VerifyRequest("GET", "/v1/metrics"),
			ghttp.VerifyHeaderKV("Authorization", "Bearer "+token),
			handleReq,
		))
	})

	ginkgo.AfterEach(func() {
		server.Close()
	})

	tryGetMetrics := func() map[string]string {
		m, err := getWithToken(server.URL())
		if err != nil {
			ginkgo.Fail("failed to get metrics: " + err.Error())
		}

		return m
	}

	ginkgo.It("should serve metrics", func() {
		gomega.Expect(tryGetMetrics()).
			To(gomega.SatisfyAll(
				gomega.HaveKeyWithValue("watchtower_containers_updated", "0"),
				gomega.HaveKeyWithValue("watchtower_containers_restarted", "0"),
				gomega.HaveKeyWithValue("watchtower_containers_restarted_total", "0"),
			))

		metric := &metrics.Metric{
			Scanned:   4,
			Updated:   3,
			Failed:    1,
			Restarted: 2,
		}

		metrics.Default().RegisterScan(metric)
		gomega.Eventually(metrics.Default().QueueIsEmpty).Should(gomega.BeTrue())

		gomega.Eventually(tryGetMetrics).Should(gomega.SatisfyAll(
			gomega.HaveKeyWithValue("watchtower_containers_updated", "3"),
			gomega.HaveKeyWithValue("watchtower_containers_failed", "1"),
			gomega.HaveKeyWithValue("watchtower_containers_scanned", "4"),
			gomega.HaveKeyWithValue("watchtower_containers_restarted", "2"),
			gomega.HaveKeyWithValue("watchtower_containers_restarted_total", "2"),
			gomega.HaveKeyWithValue("watchtower_scans_total", "1"),
			gomega.HaveKeyWithValue("watchtower_scans_skipped_total", "0"),
		))

		// Register another scan with restarted containers to test total accumulation
		metric2 := &metrics.Metric{
			Scanned:   2,
			Updated:   1,
			Failed:    0,
			Restarted: 1,
		}

		metrics.Default().RegisterScan(metric2)
		gomega.Eventually(metrics.Default().QueueIsEmpty).Should(gomega.BeTrue())

		gomega.Eventually(tryGetMetrics).Should(gomega.SatisfyAll(
			gomega.HaveKeyWithValue("watchtower_containers_restarted", "1"),
			gomega.HaveKeyWithValue("watchtower_containers_restarted_total", "3"),
			gomega.HaveKeyWithValue("watchtower_scans_total", "2"),
		))

		for range 3 {
			metrics.Default().RegisterScan(nil)
		}

		gomega.Eventually(metrics.Default().QueueIsEmpty).Should(gomega.BeTrue())

		gomega.Eventually(tryGetMetrics).Should(gomega.SatisfyAll(
			gomega.HaveKeyWithValue("watchtower_scans_total", "5"),
			gomega.HaveKeyWithValue("watchtower_scans_skipped_total", "3"),
		))
	})
})
