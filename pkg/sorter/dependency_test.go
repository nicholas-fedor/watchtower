package sorter

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainerTypes "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var _ = ginkgo.Describe("DependencySorter", func() {
	ginkgo.Describe("Sort", func() {
		ginkgo.It("should sort containers with no dependencies", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c3 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c3.EXPECT().Name().Return("c3")
			c3.EXPECT().ID().Return(types.ContainerID("id-c3"))
			c3.EXPECT().Links().Return(nil)
			c3.EXPECT().IsWatchtower().Return(false)
			c3.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c3"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2, c3}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(3))
			// Order may vary since no dependencies
		})

		ginkgo.It("should sort containers with simple dependencies", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(2))
			gomega.Expect(containers[0].Name()).To(gomega.Equal("c2"))
			gomega.Expect(containers[1].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should sort containers with complex dependency chains", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2", "c3"})
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return([]string{"c3"})
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c3 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c3.EXPECT().Name().Return("c3")
			c3.EXPECT().ID().Return(types.ContainerID("id-c3"))
			c3.EXPECT().Links().Return(nil)
			c3.EXPECT().IsWatchtower().Return(false)
			c3.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c3"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2, c3}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(3))
			gomega.Expect(containers[0].Name()).To(gomega.Equal("c3"))
			gomega.Expect(containers[1].Name()).To(gomega.Equal("c2"))
			gomega.Expect(containers[2].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should detect circular dependencies", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().Links().Return([]string{"c1"})
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("c1 -> c2 -> c1"))
		})

		ginkgo.It("should place Watchtower containers last", func() {
			watchtower := mocks.NewMockContainer(ginkgo.GinkgoT())
			watchtower.EXPECT().Name().Return("watchtower")
			watchtower.EXPECT().IsWatchtower().Return(true)
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{watchtower, c1, c2}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(3))
			gomega.Expect(containers[0].Name()).To(gomega.Equal("c2"))
			gomega.Expect(containers[1].Name()).To(gomega.Equal("c1"))
			gomega.Expect(containers[2].Name()).To(gomega.Equal("watchtower"))
		})

		ginkgo.It("should handle empty container list", func() {
			containers := []types.Container{}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.BeEmpty())
		})

		ginkgo.It("should handle single container", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(1))
			gomega.Expect(containers[0].Name()).To(gomega.Equal("c1"))
		})
	})

	ginkgo.Describe("sortByDependencies", func() {
		ginkgo.It("should sort containers with no dependencies", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
		})

		ginkgo.It("should sort containers with simple dependencies", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c2"))
			gomega.Expect(result[1].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should sort containers with complex chains", func() {
			a := mocks.NewMockContainer(ginkgo.GinkgoT())
			a.EXPECT().Name().Return("a")
			a.EXPECT().ID().Return(types.ContainerID("id-a"))
			a.EXPECT().Links().Return([]string{"b"})
			a.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/a"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			b := mocks.NewMockContainer(ginkgo.GinkgoT())
			b.EXPECT().Name().Return("b")
			b.EXPECT().ID().Return(types.ContainerID("id-b"))
			b.EXPECT().Links().Return([]string{"c"})
			b.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/b"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c := mocks.NewMockContainer(ginkgo.GinkgoT())
			c.EXPECT().Name().Return("c")
			c.EXPECT().ID().Return(types.ContainerID("id-c"))
			c.EXPECT().Links().Return(nil)
			c.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{a, b, c}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(3))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c"))
			gomega.Expect(result[1].Name()).To(gomega.Equal("b"))
			gomega.Expect(result[2].Name()).To(gomega.Equal("a"))
		})

		ginkgo.It("should detect circular dependencies", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().Links().Return([]string{"c1"})
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			_, err := sortByDependencies(containers)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("c1 -> c2 -> c1"))
		})

		ginkgo.It("should handle empty list", func() {
			containers := []types.Container{}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.BeEmpty())
		})

		ginkgo.It("should handle single container", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(1))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should handle disconnected components", func() {
			// Component 1: A -> B
			a := mocks.NewMockContainer(ginkgo.GinkgoT())
			a.EXPECT().Name().Return("a")
			a.EXPECT().ID().Return(types.ContainerID("id-a"))
			a.EXPECT().Links().Return([]string{"b"})
			a.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/a"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			b := mocks.NewMockContainer(ginkgo.GinkgoT())
			b.EXPECT().Name().Return("b")
			b.EXPECT().ID().Return(types.ContainerID("id-b"))
			b.EXPECT().Links().Return(nil)
			b.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/b"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})

			// Component 2: C -> D
			c := mocks.NewMockContainer(ginkgo.GinkgoT())
			c.EXPECT().Name().Return("c")
			c.EXPECT().ID().Return(types.ContainerID("id-c"))
			c.EXPECT().Links().Return([]string{"d"})
			c.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			d := mocks.NewMockContainer(ginkgo.GinkgoT())
			d.EXPECT().Name().Return("d")
			d.EXPECT().ID().Return(types.ContainerID("id-d"))
			d.EXPECT().Links().Return(nil)
			d.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/d"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})

			containers := []types.Container{a, b, c, d}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(4))

			resultNames := make([]string, len(result))
			for i, c := range result {
				resultNames[i] = c.Name()
			}

			assertOrderBefore(resultNames, "b", "a")
			assertOrderBefore(resultNames, "d", "c")
		})

		ginkgo.It("should detect self-referencing containers as cycles", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"c1"}) // Self-reference
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})

			containers := []types.Container{c1}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).To(gomega.HaveOccurred()) // Self-reference creates a cycle
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("c1 -> c1"))
			gomega.Expect(result).To(gomega.BeNil())
		})

		ginkgo.It("should handle containers with service names", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"web"}) // Link to service name
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/container1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})

			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("container2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/container2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{"com.docker.compose.service": "web"}}})

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			gomega.Expect(result[0].Name()).To(gomega.Equal("container2")) // web service
			gomega.Expect(result[1].Name()).To(gomega.Equal("container1")) // depends on web
		})
	})
})

var _ = ginkgo.Describe("ResolveContainerIdentifier", func() {
	ginkgo.It("should return service name when present", func() {
		mockContainer := mocks.NewMockContainer(ginkgo.GinkgoT())
		mockContainer.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
			Config: &dockerContainerTypes.Config{
				Labels: map[string]string{"com.docker.compose.service": "web"},
			},
		})
		result := container.ResolveContainerIdentifier(mockContainer)
		gomega.Expect(result).To(gomega.Equal("web"))
	})

	ginkgo.It("should return container name when no service label", func() {
		mockContainer := mocks.NewMockContainer(ginkgo.GinkgoT())
		mockContainer.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
			Config: &dockerContainerTypes.Config{
				Labels: map[string]string{},
			},
		})
		mockContainer.EXPECT().Name().Return("container1")
		result := container.ResolveContainerIdentifier(mockContainer)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})

	ginkgo.It("should return container name when labels are nil", func() {
		mockContainer := mocks.NewMockContainer(ginkgo.GinkgoT())
		mockContainer.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
			Config: &dockerContainerTypes.Config{
				Labels: nil,
			},
		})
		mockContainer.EXPECT().Name().Return("container1")
		result := container.ResolveContainerIdentifier(mockContainer)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})

	ginkgo.It("should return container name when service label is empty", func() {
		mockContainer := mocks.NewMockContainer(ginkgo.GinkgoT())
		mockContainer.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
			Config: &dockerContainerTypes.Config{
				Labels: map[string]string{"com.docker.compose.service": ""},
			},
		})
		mockContainer.EXPECT().Name().Return("container1")
		result := container.ResolveContainerIdentifier(mockContainer)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})
})

func assertOrderBefore(names []string, first, second string) {
	gomega.Expect(indexOf(names, first)).To(gomega.BeNumerically("<", indexOf(names, second)))
}

func indexOf(names []string, target string) int {
	for i, name := range names {
		if name == target {
			return i
		}
	}

	return -1
}
