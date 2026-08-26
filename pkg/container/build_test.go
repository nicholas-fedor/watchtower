package container

import (
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("confinedDockerfile", func() {
	ginkgo.It("defaults empty to Dockerfile", func() {
		got, err := confinedDockerfile("")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(got).To(gomega.Equal(DefaultGitDockerfile))
	})

	ginkgo.It("accepts a nested relative path", func() {
		got, err := confinedDockerfile("build/docker/Dockerfile")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(got).To(gomega.Equal("build/docker/Dockerfile"))
	})

	ginkgo.It("rejects path escape and absolute paths", func() {
		_, err := confinedDockerfile("../Dockerfile")
		gomega.Expect(err).To(gomega.MatchError(errGitDockerfileEscape))
		_, err = confinedDockerfile("/etc/passwd")
		gomega.Expect(err).To(gomega.MatchError(errGitDockerfileEscape))
	})
})

var _ = ginkgo.Describe("redactedRemote", func() {
	ginkgo.It("replaces a password and leaves other URLs unchanged", func() {
		gomega.Expect(redactedRemote("https://user:secret@github.com/org/app.git#abc")).
			To(gomega.Equal("https://xxxxx:xxxxx@github.com/org/app.git#abc"))
		gomega.Expect(redactedRemote("https://github.com/org/app.git")).
			To(gomega.Equal("https://github.com/org/app.git"))
		gomega.Expect(redactedRemote("://bad")).To(gomega.Equal("://bad"))
	})
})

var _ = ginkgo.Describe("consumeBuildStream", func() {
	ginkgo.It("returns the last aux image ID", func() {
		body := strings.NewReader(`{"stream":"Step 1/1"}
{"aux":{"ID":"sha256:first"}}
{"aux":{"ID":"sha256:last"}}
`)
		id, err := consumeBuildStream(body)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(id).To(gomega.Equal(types.ImageID("sha256:last")))
	})

	ginkgo.It("returns a build error from the stream", func() {
		_, err := consumeBuildStream(strings.NewReader(`{"error":"failed to solve"}`))
		gomega.Expect(err).To(gomega.MatchError(errImageBuildFailed))
	})

	ginkgo.It("returns a decode error for invalid JSON", func() {
		_, err := consumeBuildStream(strings.NewReader("{"))
		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("read build stream:"))
	})

	ginkgo.It("ignores a string aux payload from BuildKit", func() {
		body := strings.NewReader(`{"aux":"trace"}
{"aux":{"ID":"sha256:last"}}
`)
		id, err := consumeBuildStream(body)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(id).To(gomega.Equal(types.ImageID("sha256:last")))
	})
})
