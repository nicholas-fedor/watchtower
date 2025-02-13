package container_test

import (
	wt "github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("container utils", func() {
	ginkgo.Describe("ShortID", func() {
		ginkgo.When("given a normal image ID", func() {
			ginkgo.When("it contains a sha256 prefix", func() {
				ginkgo.It("should return that ID in short version", func() {
					actual := shortID("sha256:0123456789abcd00000000001111111111222222222233333333334444444444")
					gomega.Expect(actual).To(gomega.Equal("0123456789ab"))
				})
			})
			ginkgo.When("it doesn't contain a prefix", func() {
				ginkgo.It("should return that ID in short version", func() {
					actual := shortID("0123456789abcd00000000001111111111222222222233333333334444444444")
					gomega.Expect(actual).To(gomega.Equal("0123456789ab"))
				})
			})
		})
		ginkgo.When("given a short image ID", func() {
			ginkgo.When("it contains no prefix", func() {
				ginkgo.It("should return the same string", func() {
					gomega.Expect(shortID("0123456789ab")).To(gomega.Equal("0123456789ab"))
				})
			})
			ginkgo.When("it contains a the sha256 prefix", func() {
				ginkgo.It("should return the ID without the prefix", func() {
					gomega.Expect(shortID("sha256:0123456789ab")).To(gomega.Equal("0123456789ab"))
				})
			})
		})
		ginkgo.When("given an ID with an unknown prefix", func() {
			ginkgo.It("should return a short version of that ID including the prefix", func() {
				gomega.Expect(shortID("md5:0123456789ab")).To(gomega.Equal("md5:0123456789ab"))
				gomega.Expect(shortID("md5:0123456789abcdefg")).To(gomega.Equal("md5:0123456789ab"))
				gomega.Expect(shortID("md5:01")).To(gomega.Equal("md5:01"))
			})
		})
	})
})

func shortID(id string) string {
	// Proxy to the types implementation, relocated due to package dependency resolution
	return wt.ImageID(id).ShortID()
}
