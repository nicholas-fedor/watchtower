package compose

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Compose", func() {
	ginkgo.Describe("ParseDependsOnLabel", func() {
		ginkgo.It("returns nil for empty label", func() {
			result := ParseDependsOnLabel("")
			gomega.Expect(result).To(gomega.BeNil())
		})

		ginkgo.It("parses single service", func() {
			result := ParseDependsOnLabel("postgres")
			gomega.Expect(result).To(gomega.Equal([]string{"postgres"}))
		})

		ginkgo.It("parses multiple services", func() {
			result := ParseDependsOnLabel("postgres,redis")
			gomega.Expect(result).To(gomega.Equal([]string{"postgres", "redis"}))
		})

		ginkgo.It("trims whitespace", func() {
			result := ParseDependsOnLabel(" postgres , redis ")
			gomega.Expect(result).To(gomega.Equal([]string{"postgres", "redis"}))
		})

		ginkgo.It("parses colon-separated format", func() {
			result := ParseDependsOnLabel("postgres:service_started:required,redis:service_healthy")
			gomega.Expect(result).To(gomega.Equal([]string{"postgres", "redis"}))
		})

		ginkgo.It("ignores empty parts", func() {
			result := ParseDependsOnLabel("postgres,,redis")
			gomega.Expect(result).To(gomega.Equal([]string{"postgres", "redis"}))
		})
	})

	ginkgo.Describe("GetServiceName", func() {
		ginkgo.It("returns empty string for nil labels", func() {
			result := GetServiceName(nil)
			gomega.Expect(result).To(gomega.Equal(""))
		})

		ginkgo.It("returns empty string when label not present", func() {
			labels := map[string]string{"other": "value"}
			result := GetServiceName(labels)
			gomega.Expect(result).To(gomega.Equal(""))
		})

		ginkgo.It("returns service name when label present", func() {
			labels := map[string]string{ComposeServiceLabel: "web"}
			result := GetServiceName(labels)
			gomega.Expect(result).To(gomega.Equal("web"))
		})
	})
})
