package sorter

import (
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainer "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var _ = ginkgo.Describe("DependencySorter", func() {
	ginkgo.Describe("Sort", func() {
		ginkgo.It("should sort containers with no dependencies", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c3 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c3.EXPECT().Name().Return("c3")
			c3.EXPECT().ID().Return(types.ContainerID("id-c3"))
			c3.EXPECT().Links().Return(nil)
			c3.EXPECT().IsWatchtower().Return(false)
			c3.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c3"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2, c3}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(3))
			// Order may vary since no dependencies
		})

		ginkgo.It("should sort containers with simple dependencies", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(2))
			gomega.Expect(containers[0].Name()).To(gomega.Equal("c2"))
			gomega.Expect(containers[1].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should sort containers with complex dependency chains", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2", "c3"})
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return([]string{"c3"})
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c3 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c3.EXPECT().Name().Return("c3")
			c3.EXPECT().ID().Return(types.ContainerID("id-c3"))
			c3.EXPECT().Links().Return(nil)
			c3.EXPECT().IsWatchtower().Return(false)
			c3.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c3"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
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
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().Links().Return([]string{"c1"})
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			ds := DependencySorter{}
			err := ds.Sort(containers)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("c1 -> c2 -> c1"))
		})

		ginkgo.It("should place Watchtower containers last", func() {
			watchtower := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			watchtower.EXPECT().Name().Return("watchtower")
			watchtower.EXPECT().IsWatchtower().Return(true)
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().IsWatchtower().Return(false)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
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
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().IsWatchtower().Return(false)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
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
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
		})

		ginkgo.It("should sort containers with simple dependencies", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c2"))
			gomega.Expect(result[1].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should sort containers with complex chains", func() {
			a := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			a.EXPECT().Name().Return("a")
			a.EXPECT().ID().Return(types.ContainerID("id-a"))
			a.EXPECT().Links().Return([]string{"b"})
			a.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/a"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			b := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			b.EXPECT().Name().Return("b")
			b.EXPECT().ID().Return(types.ContainerID("id-b"))
			b.EXPECT().Links().Return([]string{"c"})
			b.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/b"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c.EXPECT().Name().Return("c")
			c.EXPECT().ID().Return(types.ContainerID("id-c"))
			c.EXPECT().Links().Return(nil)
			c.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			containers := []types.Container{a, b, c}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(3))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c"))
			gomega.Expect(result[1].Name()).To(gomega.Equal("b"))
			gomega.Expect(result[2].Name()).To(gomega.Equal("a"))
		})

		ginkgo.It("should detect circular dependencies", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"c2"})
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().Links().Return([]string{"c1"})
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
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
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			containers := []types.Container{c1}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(1))
			gomega.Expect(result[0].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should handle disconnected components", func() {
			// Component 1: A -> B
			a := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			a.EXPECT().Name().Return("a")
			a.EXPECT().ID().Return(types.ContainerID("id-a"))
			a.EXPECT().Links().Return([]string{"b"})
			a.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/a"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			b := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			b.EXPECT().Name().Return("b")
			b.EXPECT().ID().Return(types.ContainerID("id-b"))
			b.EXPECT().Links().Return(nil)
			b.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/b"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			// Component 2: C -> D
			c := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c.EXPECT().Name().Return("c")
			c.EXPECT().ID().Return(types.ContainerID("id-c"))
			c.EXPECT().Links().Return([]string{"d"})
			c.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
			d := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			d.EXPECT().Name().Return("d")
			d.EXPECT().ID().Return(types.ContainerID("id-d"))
			d.EXPECT().Links().Return(nil)
			d.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/d"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

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
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"c1"}) // Self-reference
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			containers := []types.Container{c1}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).To(gomega.HaveOccurred()) // Self-reference creates a cycle
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("c1 -> c1"))
			gomega.Expect(result).To(gomega.BeNil())
		})

		ginkgo.It("should handle containers with service names", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1")
			c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
			c1.EXPECT().Links().Return([]string{"web"}) // Link to service name
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("container2")
			c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container2"}, Config: &dockerContainer.Config{Labels: map[string]string{"com.docker.compose.service": "web"}}})

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			gomega.Expect(result[0].Name()).To(gomega.Equal("container2")) // web service
			gomega.Expect(result[1].Name()).To(gomega.Equal("container1")) // depends on web
		})

		ginkgo.It("should handle containers with service names containing leading slashes", func() {
			// Test that containers with service names containing leading slashes are handled correctly
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1").Maybe()
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"web-service"}) // Link to normalized service name
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("container2").Maybe()
			c2.EXPECT().ID().Return(types.ContainerID("id-c2")).Maybe()
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container2"}, Config: &dockerContainer.Config{Labels: map[string]string{"com.docker.compose.service": "/web-service"}}})

				// Malformed service name with leading slash

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			gomega.Expect(result[0].Name()).
				To(gomega.Equal("container2"))
				// web-service container first
			gomega.Expect(result[1].Name()).To(gomega.Equal("container1")) // depends on web-service
		})

		ginkgo.It(
			"should handle containers with container names containing leading slashes",
			func() {
				// Test containers with container names containing leading slashes
				c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().Name().Return("container1").Maybe()
				c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
				c1.EXPECT().Links().Return([]string{"/web"}) // Link with leading slash
				c1.EXPECT().
					ContainerInfo().
					Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

				c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().Name().Return("web").Maybe()
				c2.EXPECT().ID().Return(types.ContainerID("id-c2")).Maybe()
				c2.EXPECT().Links().Return(nil)
				c2.EXPECT().
					ContainerInfo().
					Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/web"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

					// Container name with leading slash

				containers := []types.Container{c1, c2}
				result, err := sortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(result).To(gomega.HaveLen(2))
				gomega.Expect(result[0].Name()).
					To(gomega.Equal("web"))
					// web container first
				gomega.Expect(result[1].Name()).To(gomega.Equal("container1")) // depends on web
			},
		)

		ginkgo.It("should handle containers with empty names falling back to IDs", func() {
			// Test containers with empty names that fall back to container IDs
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("").Maybe() // Empty name
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"id-c2"}) // Link to other container's ID
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: ""}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("").Maybe() // Empty name
			c2.EXPECT().ID().Return(types.ContainerID("id-c2")).Maybe()
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: ""}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			gomega.Expect(result[0].ID()).
				To(gomega.Equal(types.ContainerID("id-c2")))
				// dependency first
			gomega.Expect(result[1].ID()).
				To(gomega.Equal(types.ContainerID("id-c1")))
			// dependent second
		})

		ginkgo.It("should handle containers with malformed link targets", func() {
			// Test containers with links to non-existent containers
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1").Maybe()
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().
				Links().
				Return([]string{"non-existent", "/also-non-existent"})
				// Links to non-existent containers
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("container2").Maybe()
			c2.EXPECT().ID().Return(types.ContainerID("id-c2")).Maybe()
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container2"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			// Order may vary since no valid dependencies
		})

		ginkgo.It("should handle containers with network mode dependencies", func() {
			// Test containers with network mode dependencies
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1").Maybe()
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"web"}) // Mock includes network dependency
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container1"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("web").Maybe()
			c2.EXPECT().ID().Return(types.ContainerID("id-c2")).Maybe()
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/web"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(2))
			gomega.Expect(result[0].Name()).To(gomega.Equal("web"))        // web container first
			gomega.Expect(result[1].Name()).To(gomega.Equal("container1")) // depends on web
		})

		ginkgo.It("should handle containers with mixed dependency sources", func() {
			// Test containers with dependencies from labels, links, and network mode
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("app").Maybe()
			c1.EXPECT().ID().Return(types.ContainerID("id-app")).Maybe()
			c1.EXPECT().
				Links().
				Return([]string{"db", "cache", "redis", "proxy"})
				// Mock all dependencies
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/app"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("db").Maybe()
			c2.EXPECT().ID().Return(types.ContainerID("id-db")).Maybe()
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/db"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})

			c3 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c3.EXPECT().Name().Return("cache").Maybe()
			c3.EXPECT().ID().Return(types.ContainerID("id-cache")).Maybe()
			c3.EXPECT().Links().Return(nil)
			c3.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/cache"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})

			c4 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c4.EXPECT().Name().Return("redis").Maybe()
			c4.EXPECT().ID().Return(types.ContainerID("id-redis")).Maybe()
			c4.EXPECT().Links().Return(nil)
			c4.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/redis"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})

			c5 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c5.EXPECT().Name().Return("proxy").Maybe()
			c5.EXPECT().ID().Return(types.ContainerID("id-proxy")).Maybe()
			c5.EXPECT().Links().Return(nil)
			c5.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/proxy"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})

			containers := []types.Container{c1, c2, c3, c4, c5}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(5))
			// Dependencies should come before dependents
			resultNames := make([]string, len(result))
			for i, c := range result {
				resultNames[i] = c.Name()
			}
			// app should be last
			gomega.Expect(resultNames[len(resultNames)-1]).To(gomega.Equal("app"))
		})

		ginkgo.It("should handle containers with self-referencing dependencies", func() {
			// Test containers that reference themselves (should detect as cycle)
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1").Maybe()
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"container1"}) // Self-reference
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/container1"}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			containers := []types.Container{c1}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("container1 -> container1"))
			gomega.Expect(result).To(gomega.BeNil())
		})

		ginkgo.It("should handle large number of containers with no dependencies", func() {
			// Test performance and correctness with many containers
			containers := make([]types.Container, 100)
			for i := range 100 {
				c := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				c.EXPECT().Name().Return(fmt.Sprintf("container%d", i)).Maybe()
				c.EXPECT().ID().Return(types.ContainerID(fmt.Sprintf("id-c%d", i))).Maybe()
				c.EXPECT().Links().Return(nil)
				c.EXPECT().
					ContainerInfo().
					Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: fmt.Sprintf("/container%d", i)}, Config: &dockerContainer.Config{Labels: map[string]string{}}})
				containers[i] = c
			}

			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(result).To(gomega.HaveLen(100))
		})

		ginkgo.It("should handle containers with empty identifiers", func() {
			// This tests that containers with empty names fall back to using container ID as identifier
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("") // Empty name
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return(nil)
			c1.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: ""}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("") // Empty name
			c2.EXPECT().ID().Return(types.ContainerID("id-c2")).Maybe()
			c2.EXPECT().Links().Return(nil)
			c2.EXPECT().
				ContainerInfo().
				Return(&dockerContainer.InspectResponse{ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: ""}, Config: &dockerContainer.Config{Labels: map[string]string{}}})

			containers := []types.Container{c1, c2}
			result, err := sortByDependencies(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred()) // Should not detect false cycle
			gomega.Expect(result).To(gomega.HaveLen(2))
		})

		ginkgo.It("should handle cross-project dependency resolution with ambiguous names", func() {
			// app depending on "db"
			app := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			app.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/app"},
				Config: &dockerContainer.Config{
					Labels: map[string]string{"com.centurylinklabs.watchtower.depends-on": "db"},
				},
			})
			app.EXPECT().Name().Return("app").Maybe()
			app.EXPECT().ID().Return(types.ContainerID("id-app")).Maybe()
			app.EXPECT().Links().Return([]string{"db"})

			// db1 from project1
			db1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			db1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/project1_db_1"},
				Config: &dockerContainer.Config{Labels: map[string]string{
					"com.docker.compose.service": "db",
					"com.docker.compose.project": "project1",
				}},
			})
			db1.EXPECT().Name().Return("project1_db_1").Maybe()
			db1.EXPECT().ID().Return(types.ContainerID("id-db1")).Maybe()
			db1.EXPECT().Links().Return(nil)

			// db2 from project2
			db2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			db2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/project2_db_1"},
				Config: &dockerContainer.Config{Labels: map[string]string{
					"com.docker.compose.service": "db",
					"com.docker.compose.project": "project2",
				}},
			})
			db2.EXPECT().Name().Return("project2_db_1").Maybe()
			db2.EXPECT().ID().Return(types.ContainerID("id-db2")).Maybe()
			db2.EXPECT().Links().Return(nil)

			containers := []types.Container{app, db1, db2}

			containerMap, indegree, adjacency, _, err := buildDependencyGraph(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Graph builds successfully, but no dependency resolved since "db" doesn't match "project1-db" or "project2-db"
			gomega.Expect(containerMap).To(gomega.HaveLen(3))
			gomega.Expect(indegree["app"]).To(gomega.Equal(0))
			gomega.Expect(adjacency).To(gomega.BeEmpty())
		})

		ginkgo.It("should detect circular dependencies via watchtower labels", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
				Config: &dockerContainer.Config{
					Labels: map[string]string{"com.centurylinklabs.watchtower.depends-on": "c2"},
				},
			})
			c1.EXPECT().Name().Return("c1").Maybe()
			c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
			c1.EXPECT().Links().Return([]string{"c2"})

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"},
				Config: &dockerContainer.Config{
					Labels: map[string]string{"com.centurylinklabs.watchtower.depends-on": "c1"},
				},
			})
			c2.EXPECT().Name().Return("c2")
			c2.EXPECT().Links().Return([]string{"c1"})

			containers := []types.Container{c1, c2}

			_, err := sortByDependencies(containers)
			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("circular reference detected"))
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("c1 -> c2 -> c1"))
		})

		ginkgo.It("should handle dependencies to filtered containers", func() {
			app := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			app.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/app"},
				Config: &dockerContainer.Config{
					Labels: map[string]string{"com.centurylinklabs.watchtower.depends-on": "db"},
				},
			})
			app.EXPECT().Name().Return("app").Maybe()
			app.EXPECT().ID().Return(types.ContainerID("id-app")).Maybe()
			app.EXPECT().Links().Return([]string{"db"})

			// Only app in containers list, db not included
			containers := []types.Container{app}

			containerMap, indegree, _, _, err := buildDependencyGraph(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containerMap).To(gomega.HaveLen(1))
			gomega.Expect(indegree["app"]).To(gomega.Equal(0))
		})
	})
})

var _ = ginkgo.Describe("ResolveContainerIdentifier", func() {
	ginkgo.It("should return service name when present", func() {
		mockContainer := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		mockContainer.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			Config: &dockerContainer.Config{
				Labels: map[string]string{"com.docker.compose.service": "web"},
			},
		})
		result := container.ResolveContainerIdentifier(mockContainer)
		gomega.Expect(result).To(gomega.Equal("web"))
	})

	ginkgo.It("should return container name when no service label", func() {
		mockContainer := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		mockContainer.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			Config: &dockerContainer.Config{
				Labels: map[string]string{},
			},
		})
		mockContainer.EXPECT().Name().Return("container1")
		result := container.ResolveContainerIdentifier(mockContainer)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})

	ginkgo.It("should return container name when labels are nil", func() {
		mockContainer := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		mockContainer.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			Config: &dockerContainer.Config{
				Labels: nil,
			},
		})
		mockContainer.EXPECT().Name().Return("container1")
		result := container.ResolveContainerIdentifier(mockContainer)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})

	ginkgo.It("should return container name when service label is empty", func() {
		mockContainer := mockTypes.NewMockContainer(ginkgo.GinkgoT())
		mockContainer.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
			Config: &dockerContainer.Config{
				Labels: map[string]string{"com.docker.compose.service": ""},
			},
		})
		mockContainer.EXPECT().Name().Return("container1")
		result := container.ResolveContainerIdentifier(mockContainer)
		gomega.Expect(result).To(gomega.Equal("container1"))
	})
})

var _ = ginkgo.Describe("Identifier Collision Issues", func() {
	ginkgo.Describe("ResolveContainerIdentifier", func() {
		ginkgo.It(
			"should return different identifiers for containers from different projects with same service name",
			func() {
				// Container from project "app1" with service "web"
				mockContainer1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				mockContainer1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service":     "web",
							"com.docker.compose.project":     "app1",
							"com.docker.compose.version":     "3.8",
							"com.docker.compose.config-hash": "abc123",
						},
					},
				})

				// Container from project "app2" with same service "web"
				mockContainer2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				mockContainer2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service":     "web",
							"com.docker.compose.project":     "app2",
							"com.docker.compose.version":     "3.8",
							"com.docker.compose.config-hash": "def456",
						},
					},
				})

				result1 := container.ResolveContainerIdentifier(mockContainer1)
				result2 := container.ResolveContainerIdentifier(mockContainer2)

				// Containers from different projects should return different identifiers
				gomega.Expect(result1).To(gomega.Equal("app1-web"))
				gomega.Expect(result2).To(gomega.Equal("app2-web"))
				gomega.Expect(result1).ToNot(gomega.Equal(result2))
			},
		)
	})

	ginkgo.Describe("buildDependencyGraph", func() {
		ginkgo.It(
			"should treat containers from different projects as separate entities in dependency graph",
			func() {
				// Create two containers from different projects with same service name
				c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/app1_web_1"},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service": "web",
							"com.docker.compose.project": "app1",
						},
					},
				})
				c1.EXPECT().Links().Return(nil)

				c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/app2_web_1"},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service": "web",
							"com.docker.compose.project": "app2",
						},
					},
				})
				c2.EXPECT().Links().Return(nil)

				containers := []types.Container{c1, c2}

				containerMap, indegree, _, normalizedMap, err := buildDependencyGraph(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// Containers from different projects should have different identifiers
				ident1 := container.ResolveContainerIdentifier(c1)
				ident2 := container.ResolveContainerIdentifier(c2)
				gomega.Expect(ident1).To(gomega.Equal("app1-web"))
				gomega.Expect(ident2).To(gomega.Equal("app2-web"))

				// Both containers should be in the map as separate entities
				gomega.Expect(containerMap).To(gomega.HaveLen(2))
				gomega.Expect(containerMap["app1-web"]).To(gomega.Equal(c1))
				gomega.Expect(containerMap["app2-web"]).To(gomega.Equal(c2))

				// Verify indegree reflects separate entities
				gomega.Expect(indegree).To(gomega.HaveLen(2))
				gomega.Expect(indegree["app1-web"]).To(gomega.Equal(0))
				gomega.Expect(indegree["app2-web"]).To(gomega.Equal(0))

				// The normalizedMap should show containers map to their respective identifiers
				gomega.Expect(normalizedMap).To(gomega.HaveLen(2))
				gomega.Expect(normalizedMap[c1]).To(gomega.Equal("app1-web"))
				gomega.Expect(normalizedMap[c2]).To(gomega.Equal("app2-web"))
			},
		)

		ginkgo.It(
			"should maintain backward compatibility for containers without project labels",
			func() {
				// Container with service label but no project label
				mockContainer := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				mockContainer.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service": "web",
							// No project label
						},
					},
				})

				result := container.ResolveContainerIdentifier(mockContainer)

				// Should return just the service name, same as before
				gomega.Expect(result).To(gomega.Equal("web"))
			},
		)

		ginkgo.It(
			"should support exact matching for cross-project dependencies",
			func() {
				// Container that links to exact container name
				app := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				app.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/app"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})
				app.EXPECT().Name().Return("app").Maybe()
				app.EXPECT().ID().Return(types.ContainerID("id-app")).Maybe()
				app.EXPECT().Links().Return([]string{"project1-db"})

				// DB container from project1
				db1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				db1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/project1_db_1"},
					Config: &dockerContainer.Config{
						Labels: map[string]string{
							"com.docker.compose.service": "db",
							"com.docker.compose.project": "project1",
						},
					},
				})
				db1.EXPECT().Name().Return("project1_db_1").Maybe()
				db1.EXPECT().ID().Return(types.ContainerID("id-db1")).Maybe()
				db1.EXPECT().Links().Return(nil)

				containers := []types.Container{app, db1}

				containerMap, indegree, adjacency, normalizedMap, err := buildDependencyGraph(
					containers,
				)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// Verify identifiers
				gomega.Expect(container.ResolveContainerIdentifier(app)).To(gomega.Equal("app"))
				gomega.Expect(container.ResolveContainerIdentifier(db1)).
					To(gomega.Equal("project1-db"))

				// All containers should be in the map
				gomega.Expect(containerMap).To(gomega.HaveLen(2))

				// app should have indegree 1 (depends on db1)
				gomega.Expect(indegree["app"]).To(gomega.Equal(1))

				// db1 should have app as dependent
				gomega.Expect(adjacency["project1-db"]).To(gomega.ContainElement("app"))

				// Verify normalizedMap
				gomega.Expect(normalizedMap).To(gomega.HaveLen(2))
			},
		)
	})

	ginkgo.Describe("IdentifierCollisionError", func() {
		ginkgo.It("should format error message correctly", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("container1")
			c1.EXPECT().ID().Return(types.ContainerID("id1"))

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("container2")
			c2.EXPECT().ID().Return(types.ContainerID("id2"))

			containers := []types.Container{c1, c2}

			err := IdentifierCollisionError{
				DuplicateIdentifier: "test-id",
				AffectedContainers:  containers,
			}

			expected := "identifier collision detected: 'test-id' used by containers: container1 (id1), container2 (id2)"
			gomega.Expect(err.Error()).To(gomega.Equal(expected))
		})
	})

	ginkgo.Describe("buildDependencyGraph with collisions", func() {
		ginkgo.It(
			"should return IdentifierCollisionError when containers have same normalized identifier",
			func() {
				c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})
				c1.EXPECT().Name().Return("c1").Maybe()
				c1.EXPECT().ID().Return(types.ContainerID("id1")).Maybe()

				c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
					ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, // Same name
					Config:            &dockerContainer.Config{Labels: map[string]string{}},
				})
				c2.EXPECT().Name().Return("c1").Maybe()
				c2.EXPECT().ID().Return(types.ContainerID("id2")).Maybe()

				containers := []types.Container{c1, c2}

				_, _, _, _, err := buildDependencyGraph(containers)

				gomega.Expect(err).To(gomega.HaveOccurred())
				var collisionErr IdentifierCollisionError
				gomega.Expect(err).To(gomega.BeAssignableToTypeOf(collisionErr))
				gomega.Expect(err.(IdentifierCollisionError).DuplicateIdentifier).
					To(gomega.Equal("c1"))
				gomega.Expect(err.(IdentifierCollisionError).AffectedContainers).
					To(gomega.HaveLen(2))
			},
		)

		ginkgo.It("should succeed when containers have different identifiers", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
				Config:            &dockerContainer.Config{Labels: map[string]string{}},
			})
			c1.EXPECT().Name().Return("c1").Maybe()
			c1.EXPECT().ID().Return(types.ContainerID("id1")).Maybe()
			c1.EXPECT().Links().Return(nil)

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c2"},
				Config:            &dockerContainer.Config{Labels: map[string]string{}},
			})
			c2.EXPECT().Name().Return("c2").Maybe()
			c2.EXPECT().ID().Return(types.ContainerID("id2")).Maybe()
			c2.EXPECT().Links().Return(nil)

			containers := []types.Container{c1, c2}

			containerMap, indegree, adjacency, normalizedMap, err := buildDependencyGraph(
				containers,
			)

			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containerMap).To(gomega.HaveLen(2))
			gomega.Expect(indegree).To(gomega.HaveLen(2))
			gomega.Expect(adjacency).To(gomega.BeEmpty()) // No links set up
			gomega.Expect(normalizedMap).To(gomega.HaveLen(2))
		})
	})

	ginkgo.Describe("sortByDependencies with collisions", func() {
		ginkgo.It("should propagate IdentifierCollisionError from buildDependencyGraph", func() {
			c1 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"},
				Config:            &dockerContainer.Config{Labels: map[string]string{}},
			})
			c1.EXPECT().Name().Return("c1").Maybe()
			c1.EXPECT().ID().Return(types.ContainerID("id1")).Maybe()

			c2 := mockTypes.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().ContainerInfo().Return(&dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{Name: "/c1"}, // Same name
				Config:            &dockerContainer.Config{Labels: map[string]string{}},
			})
			c2.EXPECT().Name().Return("c1").Maybe()
			c2.EXPECT().ID().Return(types.ContainerID("id2")).Maybe()

			containers := []types.Container{c1, c2}

			_, err := sortByDependencies(containers)

			gomega.Expect(err).To(gomega.HaveOccurred())
			var collisionErr IdentifierCollisionError
			gomega.Expect(err).To(gomega.BeAssignableToTypeOf(collisionErr))
		})
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
