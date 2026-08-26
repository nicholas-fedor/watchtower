package compose

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/rs/zerolog"
)

func testLog() *zerolog.Logger {
	n := zerolog.Nop()

	return &n
}

var _ = ginkgo.Describe("Compose", func() {
	ginkgo.DescribeTable(
		"ParseDependsOnLabel",
		func(input string, expected []string) {
			result := ParseDependsOnLabel(testLog(), input)
			gomega.Expect(result).To(gomega.Equal(expected))
		},
		ginkgo.Entry("returns nil for empty label", "", nil),
		ginkgo.Entry("parses single service", "postgres", []string{"postgres"}),
		ginkgo.Entry("parses multiple services", "postgres,redis", []string{"postgres", "redis"}),
		ginkgo.Entry("trims whitespace", " postgres , redis ", []string{"postgres", "redis"}),
		ginkgo.Entry(
			"parses colon-separated format",
			"postgres:service_started:required,redis:service_healthy",
			[]string{"postgres", "redis"},
		),
		ginkgo.Entry("ignores empty parts", "postgres,,redis", []string{"postgres", "redis"}),
		ginkgo.Entry(
			"parses JSON format",
			`{"database":{"condition":"service_started"}}`,
			[]string{"database"},
		),
		ginkgo.Entry(
			"parses JSON format with multiple services",
			`{"database":{"condition":"service_started"},"cache":{"condition":"service_healthy"}}`,
			[]string{"cache", "database"}, // Sorted alphabetically
		),
	)

	ginkgo.DescribeTable(
		"GetServiceName",
		func(labels map[string]string, expected string) {
			result := GetServiceName(testLog(), labels)
			gomega.Expect(result).To(gomega.Equal(expected))
		},
		ginkgo.Entry("returns empty string for nil labels", nil, ""),
		ginkgo.Entry("returns empty string for empty labels", map[string]string{}, ""),
		ginkgo.Entry(
			"returns empty string when label not present",
			map[string]string{"other": "value"},
			"",
		),
		ginkgo.Entry(
			"returns service name when label present",
			map[string]string{ComposeServiceLabel: "web"},
			"web",
		),
	)

	ginkgo.DescribeTable(
		"GetProjectName",
		func(labels map[string]string, expected string) {
			result := GetProjectName(testLog(), labels)
			gomega.Expect(result).To(gomega.Equal(expected))
		},
		ginkgo.Entry("returns empty string for nil labels", nil, ""),
		ginkgo.Entry("returns empty string for empty labels", map[string]string{}, ""),
		ginkgo.Entry(
			"returns empty string when label not present",
			map[string]string{"other": "value"},
			"",
		),
		ginkgo.Entry(
			"returns project name when label present",
			map[string]string{ComposeProjectLabel: "myproject"},
			"myproject",
		),
	)

	ginkgo.DescribeTable(
		"GetContainerNumber",
		func(labels map[string]string, expected string) {
			result := GetContainerNumber(testLog(), labels)
			gomega.Expect(result).To(gomega.Equal(expected))
		},
		ginkgo.Entry("returns empty string for nil labels", nil, ""),
		ginkgo.Entry("returns empty string for empty labels", map[string]string{}, ""),
		ginkgo.Entry(
			"returns empty string when label not present",
			map[string]string{"other": "value"},
			"",
		),
		ginkgo.Entry(
			"returns container number when label present",
			map[string]string{ComposeContainerNumber: "1"},
			"1",
		),
	)
})
