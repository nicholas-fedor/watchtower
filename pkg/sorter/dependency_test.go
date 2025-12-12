package sorter

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainerTypes "github.com/docker/docker/api/types/container"

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
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
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

	ginkgo.Describe("sort", func() {
		ginkgo.It("should sort containers with no dependencies", func() {
			ds := &dependencySorter{}
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
			result, err := ds.sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
		})

		ginkgo.It("should sort containers with simple dependencies", func() {
			ds := &dependencySorter{}
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
			result, err := ds.sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c2"))
			gomega.Expect(result[1].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should sort containers with complex chains", func() {
			ds := &dependencySorter{}
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
			result, err := ds.sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(3))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c"))
			gomega.Expect(result[1].Name()).To(gomega.Equal("b"))
			gomega.Expect(result[2].Name()).To(gomega.Equal("a"))
		})

		ginkgo.It("should detect circular dependencies", func() {
			ds := &dependencySorter{}
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
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
			_, err := ds.sort(containers)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
		})

		ginkgo.It("should handle empty list", func() {
			ds := &dependencySorter{}
			containers := []types.Container{}
			result, err := ds.sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.BeEmpty())
		})

		ginkgo.It("should handle single container", func() {
			ds := &dependencySorter{}
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1}
			result, err := ds.sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(1))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c1"))
		})
	})

	ginkgo.Describe("visit", func() {
		ginkgo.It("should visit container with no dependencies", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			ds := &dependencySorter{
				unvisited: []types.Container{c1},
				marked:    map[string]bool{},
				sorted:    []types.Container{},
			}
			c := c1
			err := ds.visit(c)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(ds.sorted).To(gomega.HaveLen(1))
			gomega.Expect(ds.sorted[0].Name()).To(gomega.Equal("c1"))
			gomega.Expect(ds.unvisited).To(gomega.BeEmpty())
		})

		ginkgo.It("should visit container with dependencies", func() {
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			ds := &dependencySorter{
				unvisited: []types.Container{c1, c2},
				marked:    map[string]bool{},
				sorted:    []types.Container{},
			}
			err := ds.visit(c1)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(ds.sorted).To(gomega.HaveLen(2))
			gomega.Expect(ds.sorted[0].Name()).To(gomega.Equal("c2"))
			gomega.Expect(ds.sorted[1].Name()).To(gomega.Equal("c1"))
			gomega.Expect(ds.unvisited).To(gomega.BeEmpty())
		})

		ginkgo.It("should detect cycle in dependencies", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
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
			ds := &dependencySorter{
				unvisited: []types.Container{c1, c2},
				marked:    map[string]bool{},
				sorted:    []types.Container{},
			}
			err := ds.visit(c1)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
		})

		ginkgo.It("should handle missing dependencies gracefully", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c3"})
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			ds := &dependencySorter{
				unvisited: []types.Container{c1},
				marked:    map[string]bool{},
				sorted:    []types.Container{},
			}
			err := ds.visit(c1)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(ds.sorted).To(gomega.HaveLen(1))
			gomega.Expect(ds.sorted[0].Name()).To(gomega.Equal("c1"))
			gomega.Expect(ds.unvisited).To(gomega.BeEmpty())
		})
	})

	ginkgo.Describe("findUnvisited", func() {
		ginkgo.It("should find container by name", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			ds := &dependencySorter{
				unvisited: []types.Container{c1, c2},
			}
			found := ds.findUnvisited("c1")
			gomega.Expect(found).ToNot(gomega.BeNil())
			gomega.Expect((*found).Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should return nil for non-existent container", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			ds := &dependencySorter{
				unvisited: []types.Container{c1},
			}
			found := ds.findUnvisited("c2")
			gomega.Expect(found).To(gomega.BeNil())
		})

		ginkgo.It("should handle empty unvisited list", func() {
			ds := &dependencySorter{
				unvisited: []types.Container{},
			}
			found := ds.findUnvisited("c1")
			gomega.Expect(found).To(gomega.BeNil())
		})

		ginkgo.It("should find container with service name", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1")
			c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
				ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/container1"},
				Config: &dockerContainerTypes.Config{
					Labels: map[string]string{"com.docker.compose.service": "web"},
				},
			})
			ds := &dependencySorter{
				unvisited: []types.Container{c1},
			}
			found := ds.findUnvisited("web")
			gomega.Expect(found).ToNot(gomega.BeNil())
			gomega.Expect((*found).Name()).To(gomega.Equal("container1"))
		})

		ginkgo.It("should find container with slash-prefixed name", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			ds := &dependencySorter{
				unvisited: []types.Container{c1},
			}
			found := ds.findUnvisited("/c1")
			gomega.Expect(found).ToNot(gomega.BeNil())
			gomega.Expect((*found).Name()).To(gomega.Equal("c1"))
		})
	})

	ginkgo.Describe("removeUnvisited", func() {
		ginkgo.It("should remove container from unvisited list", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1").Maybe()
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}}).
				Maybe()
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2").Maybe()
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}}).
				Maybe()
			ds := &dependencySorter{
				unvisited: []types.Container{c1, c2},
			}
			ds.removeUnvisited(c1)
			gomega.Expect(ds.unvisited).To(gomega.HaveLen(1))
			gomega.Expect(ds.unvisited[0].Name()).To(gomega.Equal("c2"))
		})

		ginkgo.It("should handle removing non-existent container", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			c3 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c3.EXPECT().Name().Return("c3")
			c3.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c3"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}})
			ds := &dependencySorter{
				unvisited: []types.Container{c1},
			}
			ds.removeUnvisited(c3)
			gomega.Expect(ds.unvisited).To(gomega.HaveLen(1))
			gomega.Expect(ds.unvisited[0].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should handle empty list", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1").Maybe()
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}}).
				Maybe()
			ds := &dependencySorter{
				unvisited: []types.Container{},
			}
			ds.removeUnvisited(c1)
			gomega.Expect(ds.unvisited).To(gomega.BeEmpty())
		})

		ginkgo.It("should remove container with service name", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1").Maybe()
			c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
				ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/container1"},
				Config: &dockerContainerTypes.Config{
					Labels: map[string]string{"com.docker.compose.service": "web"},
				},
			}).Maybe()
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2").Maybe()
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainerTypes.InspectResponse{ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainerTypes.Config{Labels: map[string]string{}}}).
				Maybe()
			ds := &dependencySorter{
				unvisited: []types.Container{c1, c2},
			}
			c1WithService := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1WithService.EXPECT().Name().Return("web").Maybe()
			c1WithService.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
				ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{Name: "/web"},
				Config: &dockerContainerTypes.Config{
					Labels: map[string]string{"com.docker.compose.service": "web"},
				},
			}).Maybe()
			ds.removeUnvisited(c1WithService)
			gomega.Expect(ds.unvisited).To(gomega.HaveLen(1))
			gomega.Expect(ds.unvisited[0].Name()).To(gomega.Equal("c2"))
		})
	})
})

var _ = ginkgo.Describe("GetContainerIdentifier", func() {
	ginkgo.It("should return service name when present", func() {
		container := mocks.NewMockContainer(ginkgo.GinkgoT())
		container.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
			Config: &dockerContainerTypes.Config{
				Labels: map[string]string{"com.docker.compose.service": "web"},
			},
		})
		result := GetContainerIdentifier(container)
		gomega.Expect(result).To(gomega.Equal("web"))
	})

	ginkgo.It("should return container name when no service label", func() {
		container := mocks.NewMockContainer(ginkgo.GinkgoT())
		container.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
			Config: &dockerContainerTypes.Config{
				Labels: map[string]string{},
			},
		})
		container.EXPECT().Name().Return("container1")
		result := GetContainerIdentifier(container)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})

	ginkgo.It("should return container name when labels are nil", func() {
		container := mocks.NewMockContainer(ginkgo.GinkgoT())
		container.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
			Config: &dockerContainerTypes.Config{
				Labels: nil,
			},
		})
		container.EXPECT().Name().Return("container1")
		result := GetContainerIdentifier(container)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})

	ginkgo.It("should return container name when service label is empty", func() {
		container := mocks.NewMockContainer(ginkgo.GinkgoT())
		container.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
			Config: &dockerContainerTypes.Config{
				Labels: map[string]string{"com.docker.compose.service": ""},
			},
		})
		container.EXPECT().Name().Return("container1")
		result := GetContainerIdentifier(container)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})
})
