package registry

import (
	"os"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("Registry credential helpers", func() {
	ginkgo.Describe("EncodedAuth", func() {
		ginkgo.It("should return repo credentials from env when set", func() {
			var err error
			expected := "eyJ1c2VybmFtZSI6ImNvbnRhaW5ycnItdXNlciIsInBhc3N3b3JkIjoiY29udGFpbnJyci1wYXNzIn0="

			err = os.Setenv("REPO_USER", "containrrr-user")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = os.Setenv("REPO_PASS", "containrrr-pass")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			config, err := EncodedEnvAuth()
			gomega.Expect(config).To(gomega.Equal(expected))
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.Describe("EncodedEnvAuth", func() {
		ginkgo.It("should return an error if repo envs are unset", func() {
			_ = os.Unsetenv("REPO_USER")
			_ = os.Unsetenv("REPO_PASS")

			_, err := EncodedEnvAuth()
			gomega.Expect(err).To(gomega.HaveOccurred())
		})
	})

	ginkgo.Describe("EncodedConfigAuth", func() {
		ginkgo.It("should return an error if file is not present", func() {
			var err error

			err = os.Setenv("DOCKER_CONFIG", "/dev/null/should-fail")
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			_, err = EncodedConfigAuth("")
			gomega.Expect(err).To(gomega.HaveOccurred())
		})
	})
})
