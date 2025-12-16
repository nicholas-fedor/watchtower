package container

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var _ = ginkgo.Describe("DetectCycles", func() {
	ginkgo.It("should return empty map for acyclic graphs", func() {
		c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c1.EXPECT().Name().Return("c1")
		c1.EXPECT().Links().Return(nil)
		c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c2.EXPECT().Name().Return("c2")
		c2.EXPECT().Links().Return([]string{"c1"})
		c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		containers := []types.Container{c1, c2}
		cycles := DetectCycles(containers)
		gomega.Expect(cycles).To(gomega.BeEmpty())
	})

	ginkgo.It("should detect simple cycles", func() {
		c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c1.EXPECT().Name().Return("c1")
		c1.EXPECT().Links().Return([]string{"c2"})
		c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c2.EXPECT().Name().Return("c2")
		c2.EXPECT().Links().Return([]string{"c1"})
		c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		containers := []types.Container{c1, c2}
		cycles := DetectCycles(containers)
		gomega.Expect(cycles).To(gomega.HaveLen(2))
		gomega.Expect(cycles).To(gomega.HaveKey("c1"))
		gomega.Expect(cycles).To(gomega.HaveKey("c2"))
	})

	ginkgo.It("should detect complex cycles", func() {
		c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c1.EXPECT().Name().Return("c1")
		c1.EXPECT().Links().Return([]string{"c2"})
		c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c2.EXPECT().Name().Return("c2")
		c2.EXPECT().Links().Return([]string{"c3"})
		c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		c3 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c3.EXPECT().Name().Return("c3")
		c3.EXPECT().Links().Return([]string{"c1"})
		c3.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c3"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		containers := []types.Container{c1, c2, c3}
		cycles := DetectCycles(containers)
		gomega.Expect(cycles).To(gomega.HaveLen(3))
		gomega.Expect(cycles).To(gomega.HaveKey("c1"))
		gomega.Expect(cycles).To(gomega.HaveKey("c2"))
		gomega.Expect(cycles).To(gomega.HaveKey("c3"))
	})

	ginkgo.It("should detect self-loops", func() {
		c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c1.EXPECT().Name().Return("c1")
		c1.EXPECT().Links().Return([]string{"c1"})
		c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		containers := []types.Container{c1}
		cycles := DetectCycles(containers)
		gomega.Expect(cycles).To(gomega.HaveLen(1))
		gomega.Expect(cycles).To(gomega.HaveKey("c1"))
	})

	ginkgo.It("should handle disconnected components", func() {
		// Acyclic component
		c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c1.EXPECT().Name().Return("c1")
		c1.EXPECT().Links().Return(nil)
		c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		// Cyclic component
		c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c2.EXPECT().Name().Return("c2")
		c2.EXPECT().Links().Return([]string{"c3"})
		c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		c3 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c3.EXPECT().Name().Return("c3")
		c3.EXPECT().Links().Return([]string{"c2"})
		c3.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c3"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		containers := []types.Container{c1, c2, c3}
		cycles := DetectCycles(containers)
		gomega.Expect(cycles).To(gomega.HaveLen(2))
		gomega.Expect(cycles).To(gomega.HaveKey("c2"))
		gomega.Expect(cycles).To(gomega.HaveKey("c3"))
		gomega.Expect(cycles).ToNot(gomega.HaveKey("c1"))
	})

	ginkgo.It("should handle empty container list", func() {
		containers := []types.Container{}
		cycles := DetectCycles(containers)
		gomega.Expect(cycles).To(gomega.BeEmpty())
	})

	ginkgo.It("should ignore unknown dependencies", func() {
		c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c1.EXPECT().Name().Return("c1")
		c1.EXPECT().
			Links().
			Return([]string{"c2", "unknown"})
			// c1 links to c2 (known) and unknown (not in list)
		c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		c2.EXPECT().Name().Return("c2")
		c2.EXPECT().Links().Return([]string{"c1"}) // c2 links back to c1, creating a cycle
		c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"},
			Config:            &dockerContainer.Config{Labels: map[string]string{}},
		})

		containers := []types.Container{
			c1,
			c2,
		} // Only c1 and c2 provided, "unknown" is not in the list
		cycles := DetectCycles(containers)
		gomega.Expect(cycles).
			To(gomega.HaveLen(2))
			// Cycle should still be detected between c1 and c2
		gomega.Expect(cycles).To(gomega.HaveKey("c1"))
		gomega.Expect(cycles).To(gomega.HaveKey("c2"))
	})
})
