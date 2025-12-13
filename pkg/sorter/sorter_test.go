package sorter_test

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainerTypes "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var _ = ginkgo.Describe("Container Sorting", func() {
	ginkgo.Describe("SortByCreated", func() {
		ginkgo.When("sorting by creation date", func() {
			ginkgo.It("sorts containers in ascending order", func() {
				now := time.Now()
				c3 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c3.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
						Created: now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
					},
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c3.EXPECT().Name().Return("c3")
				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
						Created: now.Add(-3 * time.Hour).Format(time.RFC3339Nano),
					},
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c1.EXPECT().Name().Return("c1")
				c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
						Created: now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
					},
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c2.EXPECT().Name().Return("c2")
				containers := []types.Container{c3, c1, c2}
				sorter.SortByCreated(containers)
				gomega.Expect(containers[0].Name()).To(gomega.Equal("c1"))
				gomega.Expect(containers[1].Name()).To(gomega.Equal("c2"))
				gomega.Expect(containers[2].Name()).To(gomega.Equal("c3"))
			})

			ginkgo.It("handles invalid creation dates gracefully", func() {
				now := time.Now()
				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
						Created: "invalid-date",
					},
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c1.EXPECT().Name().Return("c1").Maybe()
				c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
				c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
						Created: now.Format(time.RFC3339Nano),
					},
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c2.EXPECT().Name().Return("c2").Maybe()
				c2.EXPECT().ID().Return(types.ContainerID("id-c2")).Maybe()
				containers := []types.Container{c1, c2}
				sorter.SortByCreated(containers)
				// Invalid date uses current time, order may vary; check stability
				gomega.Expect(containers).To(gomega.HaveLen(2))
			})

			ginkgo.It("handles empty list", func() {
				containers := []types.Container{}
				sorter.SortByCreated(containers)
				gomega.Expect(containers).To(gomega.BeEmpty())
			})
		})
	})

	ginkgo.Describe("SortByDependencies", func() {
		ginkgo.When("sorting by dependencies", func() {
			ginkgo.It("sorts containers with no links first", func() {
				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().Name().Return("c1")
				c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
				c1.EXPECT().Links().Return([]string{"c2"})
				c1.EXPECT().IsWatchtower().Return(false)
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().Name().Return("c2")
				c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
				c2.EXPECT().Links().Return([]string(nil))
				c2.EXPECT().IsWatchtower().Return(false)
				c2.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				containers := []types.Container{c1, c2}
				err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.HaveLen(2))
				gomega.Expect(containers[0].Name()).To(gomega.Equal("c2")) // No links
				gomega.Expect(containers[1].Name()).To(gomega.Equal("c1")) // Depends on c2
			})

			ginkgo.It("handles multiple dependencies", func() {
				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().Name().Return("c1")
				c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
				c1.EXPECT().Links().Return([]string{"c2", "c3"})
				c1.EXPECT().IsWatchtower().Return(false)
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().Name().Return("c2")
				c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
				c2.EXPECT().Links().Return([]string{"c3"})
				c2.EXPECT().IsWatchtower().Return(false)
				c2.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c3 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c3.EXPECT().Name().Return("c3")
				c3.EXPECT().ID().Return(types.ContainerID("id-c3"))
				c3.EXPECT().Links().Return([]string(nil))
				c3.EXPECT().IsWatchtower().Return(false)
				c3.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				containers := []types.Container{c1, c2, c3}
				err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.HaveLen(3))
				gomega.Expect(containers[0].Name()).To(gomega.Equal("c3")) // No links
				gomega.Expect(containers[1].Name()).To(gomega.Equal("c2")) // Links to c3
				gomega.Expect(containers[2].Name()).To(gomega.Equal("c1")) // Links to c2, c3
			})

			ginkgo.It("detects circular references", func() {
				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().Name().Return("c1")
				c1.EXPECT().ID().Return(types.ContainerID("id-c1")).Maybe()
				c1.EXPECT().Links().Return([]string{"c2"})
				c1.EXPECT().IsWatchtower().Return(false)
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().Name().Return("c2")
				c2.EXPECT().ID().Return(types.ContainerID("id-c2")).Maybe()
				c2.EXPECT().Links().Return([]string{"c1"})
				c2.EXPECT().IsWatchtower().Return(false)
				c2.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				containers := []types.Container{c1, c2}
				err := sorter.SortByDependencies(containers)
				gomega.Expect(err).To(gomega.HaveOccurred())
				gomega.Expect(err.Error()).
					To(gomega.ContainSubstring("circular reference detected"))
				gomega.Expect(err.Error()).To(gomega.ContainSubstring("c1 -> c2 -> c1"))
			})

			ginkgo.It("handles missing dependencies gracefully", func() {
				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().Name().Return("c1")
				c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
				c1.EXPECT().Links().Return([]string{"c2"})
				c1.EXPECT().IsWatchtower().Return(false)
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c3 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c3.EXPECT().Name().Return("c3")
				c3.EXPECT().ID().Return(types.ContainerID("id-c3"))
				c3.EXPECT().Links().Return([]string(nil))
				c3.EXPECT().IsWatchtower().Return(false)
				c3.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				containers := []types.Container{c1, c3}
				err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.HaveLen(2))
				gomega.Expect(containers[0].Name()).To(gomega.Equal("c3")) // No links
				gomega.Expect(containers[1].Name()).To(gomega.Equal("c1")) // Links to missing c2
			})

			ginkgo.It("handles empty list", func() {
				containers := []types.Container{}
				err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.BeEmpty())
			})

			ginkgo.It("places Watchtower containers last", func() {
				watchtower := mocks.NewMockContainer(ginkgo.GinkgoT())
				watchtower.EXPECT().Name().Return("watchtower")
				watchtower.EXPECT().IsWatchtower().Return(true)
				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().Name().Return("c1")
				c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
				c1.EXPECT().Links().Return([]string{"c2"})
				c1.EXPECT().IsWatchtower().Return(false)
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().Name().Return("c2")
				c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
				c2.EXPECT().Links().Return([]string(nil))
				c2.EXPECT().IsWatchtower().Return(false)
				c2.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				containers := []types.Container{watchtower, c1, c2}
				err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.HaveLen(3))
				gomega.Expect(containers[0].Name()).To(gomega.Equal("c2")) // No links
				gomega.Expect(containers[1].Name()).To(gomega.Equal("c1")) // Depends on c2
				gomega.Expect(containers[2].Name()).
					To(gomega.Equal("watchtower"))
				// Watchtower last
			})

			ginkgo.It("places multiple Watchtower containers last", func() {
				watchtower1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				watchtower1.EXPECT().Name().Return("watchtower1")
				watchtower1.EXPECT().IsWatchtower().Return(true)
				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().Name().Return("c1")
				c1.EXPECT().ID().Return(types.ContainerID("id-c1"))
				c1.EXPECT().Links().Return([]string{"c2"})
				c1.EXPECT().IsWatchtower().Return(false)
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				watchtower2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				watchtower2.EXPECT().Name().Return("watchtower2")
				watchtower2.EXPECT().IsWatchtower().Return(true)
				c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().Name().Return("c2")
				c2.EXPECT().ID().Return(types.ContainerID("id-c2"))
				c2.EXPECT().Links().Return([]string(nil))
				c2.EXPECT().IsWatchtower().Return(false)
				c2.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				containers := []types.Container{watchtower1, c1, watchtower2, c2}
				err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(containers).To(gomega.HaveLen(4))
				gomega.Expect(containers[0].Name()).To(gomega.Equal("c2")) // No links
				gomega.Expect(containers[1].Name()).To(gomega.Equal("c1")) // Depends on c2
				// Watchtower containers at the end (order may vary)
				watchtowerNames := []string{containers[2].Name(), containers[3].Name()}
				gomega.Expect(watchtowerNames).To(gomega.ContainElement("watchtower1"))
				gomega.Expect(watchtowerNames).To(gomega.ContainElement("watchtower2"))
			})

			ginkgo.It("handles chained dependencies with slash-prefixed links", func() {
				testCases := []struct {
					name          string
					containers    func() []types.Container
					expectedOrder []string
				}{
					{
						name: "simple chain with slashes",
						containers: func() []types.Container {
							c := mocks.NewMockContainer(ginkgo.GinkgoT())
							c.EXPECT().Name().Return("c")
							c.EXPECT().ID().Return(types.ContainerID("id-c"))
							c.EXPECT().Links().Return([]string(nil))
							c.EXPECT().IsWatchtower().Return(false)
							c.EXPECT().ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							b := mocks.NewMockContainer(ginkgo.GinkgoT())
							b.EXPECT().Name().Return("b")
							b.EXPECT().ID().Return(types.ContainerID("id-b"))
							b.EXPECT().Links().Return([]string{"/c"})
							b.EXPECT().IsWatchtower().Return(false)
							b.EXPECT().ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							a := mocks.NewMockContainer(ginkgo.GinkgoT())
							a.EXPECT().Name().Return("a")
							a.EXPECT().ID().Return(types.ContainerID("id-a"))
							a.EXPECT().Links().Return([]string{"/b"})
							a.EXPECT().IsWatchtower().Return(false)
							a.EXPECT().ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})

							return []types.Container{c, b, a}
						},
						expectedOrder: []string{"c", "b", "a"},
					},
					{
						name: "multiple dependencies with slashes",
						containers: func() []types.Container {
							d := mocks.NewMockContainer(ginkgo.GinkgoT())
							d.EXPECT().Name().Return("d")
							d.EXPECT().ID().Return(types.ContainerID("id-d"))
							d.EXPECT().Links().Return([]string(nil))
							d.EXPECT().IsWatchtower().Return(false)
							d.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							c := mocks.NewMockContainer(ginkgo.GinkgoT())
							c.EXPECT().Name().Return("c")
							c.EXPECT().ID().Return(types.ContainerID("id-c"))
							c.EXPECT().Links().Return([]string(nil))
							c.EXPECT().IsWatchtower().Return(false)
							c.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							b := mocks.NewMockContainer(ginkgo.GinkgoT())
							b.EXPECT().Name().Return("b")
							b.EXPECT().ID().Return(types.ContainerID("id-b"))
							b.EXPECT().Links().Return([]string{"/d"})
							b.EXPECT().IsWatchtower().Return(false)
							b.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							a := mocks.NewMockContainer(ginkgo.GinkgoT())
							a.EXPECT().Name().Return("a")
							a.EXPECT().ID().Return(types.ContainerID("id-a"))
							a.EXPECT().Links().Return([]string{"/b", "/c"})
							a.EXPECT().IsWatchtower().Return(false)
							a.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})

							return []types.Container{d, c, b, a}
						},
						expectedOrder: []string{"d", "c", "b", "a"},
					},
					{
						name: "diamond dependency graph",
						containers: func() []types.Container {
							d := mocks.NewMockContainer(ginkgo.GinkgoT())
							d.EXPECT().Name().Return("d")
							d.EXPECT().ID().Return(types.ContainerID("id-d"))
							d.EXPECT().Links().Return([]string(nil))
							d.EXPECT().IsWatchtower().Return(false)
							d.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							b := mocks.NewMockContainer(ginkgo.GinkgoT())
							b.EXPECT().Name().Return("b")
							b.EXPECT().ID().Return(types.ContainerID("id-b"))
							b.EXPECT().Links().Return([]string{"/d"})
							b.EXPECT().IsWatchtower().Return(false)
							b.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							c := mocks.NewMockContainer(ginkgo.GinkgoT())
							c.EXPECT().Name().Return("c")
							c.EXPECT().ID().Return(types.ContainerID("id-c"))
							c.EXPECT().Links().Return([]string{"/d"})
							c.EXPECT().IsWatchtower().Return(false)
							c.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							a := mocks.NewMockContainer(ginkgo.GinkgoT())
							a.EXPECT().Name().Return("a")
							a.EXPECT().ID().Return(types.ContainerID("id-a"))
							a.EXPECT().Links().Return([]string{"/b", "/c"})
							a.EXPECT().IsWatchtower().Return(false)
							a.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})

							return []types.Container{d, b, c, a}
						},
						expectedOrder: []string{
							"d",
							"b",
							"c",
							"a",
						}, // D first, then B and C (order may vary), then A
					},
					{
						name: "no dependencies",
						containers: func() []types.Container {
							a := mocks.NewMockContainer(ginkgo.GinkgoT())
							a.EXPECT().Name().Return("a")
							a.EXPECT().ID().Return(types.ContainerID("id-a"))
							a.EXPECT().Links().Return([]string(nil))
							a.EXPECT().IsWatchtower().Return(false)
							a.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							b := mocks.NewMockContainer(ginkgo.GinkgoT())
							b.EXPECT().Name().Return("b")
							b.EXPECT().ID().Return(types.ContainerID("id-b"))
							b.EXPECT().Links().Return([]string(nil))
							b.EXPECT().IsWatchtower().Return(false)
							b.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							c := mocks.NewMockContainer(ginkgo.GinkgoT())
							c.EXPECT().Name().Return("c")
							c.EXPECT().ID().Return(types.ContainerID("id-c"))
							c.EXPECT().Links().Return([]string(nil))
							c.EXPECT().IsWatchtower().Return(false)
							c.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})

							return []types.Container{a, b, c}
						},
						expectedOrder: []string{"c", "b", "a"},
					},
					{
						name: "mixed slash scenarios",
						containers: func() []types.Container {
							c := mocks.NewMockContainer(ginkgo.GinkgoT())
							c.EXPECT().Name().Return("c")
							c.EXPECT().ID().Return(types.ContainerID("id-c"))
							c.EXPECT().Links().Return([]string(nil))
							c.EXPECT().IsWatchtower().Return(false)
							c.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							b := mocks.NewMockContainer(ginkgo.GinkgoT())
							b.EXPECT().Name().Return("b")
							b.EXPECT().ID().Return(types.ContainerID("id-b"))
							b.EXPECT().Links().Return([]string{"c"})
							b.EXPECT().IsWatchtower().Return(false)
							b.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})
							a := mocks.NewMockContainer(ginkgo.GinkgoT())
							a.EXPECT().Name().Return("a")
							a.EXPECT().ID().Return(types.ContainerID("id-a"))
							a.EXPECT().Links().Return([]string{"/b"})
							a.EXPECT().IsWatchtower().Return(false)
							a.EXPECT().
								ContainerInfo().
								Return(&dockerContainerTypes.InspectResponse{
									Config: &dockerContainerTypes.Config{
										Labels: map[string]string{},
									},
								})

							return []types.Container{c, b, a}
						},
						expectedOrder: []string{"c", "b", "a"},
					},
				}
				for _, tc := range testCases {
					ginkgo.By(tc.name, func() {
						containers := tc.containers()
						err := sorter.SortByDependencies(containers)
						gomega.Expect(err).ToNot(gomega.HaveOccurred())
						gomega.Expect(containers).To(gomega.HaveLen(len(tc.expectedOrder)))
						// For diamond, check that D is first, A is last, and B/C are in middle
						if tc.name == "diamond dependency graph" {
							gomega.Expect(containers[0].Name()).To(gomega.Equal("d"))
							gomega.Expect(containers[3].Name()).To(gomega.Equal("a"))
							middleNames := []string{containers[1].Name(), containers[2].Name()}
							gomega.Expect(middleNames).To(gomega.ContainElement("b"))
							gomega.Expect(middleNames).To(gomega.ContainElement("c"))
						} else {
							for i, name := range tc.expectedOrder {
								gomega.Expect(containers[i].Name()).To(gomega.Equal(name))
							}
						}
					})
				}
			})
		})
	})
})
