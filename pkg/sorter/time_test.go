package sorter

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainerTypes "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/types"
	"github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

var _ = ginkgo.Describe("TimeSorter", func() {
	ginkgo.Describe("Sort", func() {
		ginkgo.It("should sort containers by creation time in ascending order", func() {
			now := time.Now()
			ts := TimeSorter{}

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

			containers := []types.Container{c3, c1, c2}
			err := ts.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers[0].Name()).To(gomega.Equal("c1"))
			gomega.Expect(containers[1].Name()).To(gomega.Equal("c2"))
			gomega.Expect(containers[2].Name()).To(gomega.Equal("c3"))
		})

		ginkgo.It(
			"should handle invalid creation timestamps by using far future time as fallback",
			func() {
				now := time.Now()
				ts := TimeSorter{}

				c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
						Created: "invalid-date",
					},
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c1.EXPECT().Name().Return("c1")
				c1.EXPECT().ID().Return(types.ContainerID("id-c1"))

				c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
				c2.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
					ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
						Created: now.Format(time.RFC3339Nano),
					},
					Config: &dockerContainerTypes.Config{
						Labels: map[string]string{},
					},
				})
				c2.EXPECT().Name().Return("c2")

				containers := []types.Container{c1, c2}
				err := ts.Sort(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				// Invalid date uses far future time, so c1 (far future) should come after c2 (now)
				gomega.Expect(containers[0].Name()).To(gomega.Equal("c2"))
				gomega.Expect(containers[1].Name()).To(gomega.Equal("c1"))
			},
		)

		ginkgo.It("should handle empty slice", func() {
			ts := TimeSorter{}
			containers := []types.Container{}
			err := ts.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.BeEmpty())
		})

		ginkgo.It("should handle single container", func() {
			ts := TimeSorter{}
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
				ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
					Created: time.Now().Format(time.RFC3339Nano),
				},
				Config: &dockerContainerTypes.Config{
					Labels: map[string]string{},
				},
			}).Maybe()
			c1.EXPECT().Name().Return("c1")

			containers := []types.Container{c1}
			err := ts.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(1))
			gomega.Expect(containers[0].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should handle containers with same creation time", func() {
			now := time.Now()
			ts := TimeSorter{}

			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().ContainerInfo().Return(&dockerContainerTypes.InspectResponse{
				ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
					Created: now.Format(time.RFC3339Nano),
				},
				Config: &dockerContainerTypes.Config{
					Labels: map[string]string{},
				},
			})
			c1.EXPECT().Name().Return("c1").Maybe()

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

			containers := []types.Container{c2, c1}
			err := ts.Sort(containers)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(containers).To(gomega.HaveLen(2))
			// Order may be stable, but since times are equal, any order is fine
		})
	})
})

var _ = ginkgo.Describe("byCreated", func() {
	ginkgo.Describe("Len", func() {
		ginkgo.It("should return length of empty slice", func() {
			var bc byCreated
			gomega.Expect(bc.Len()).To(gomega.Equal(0))
		})

		ginkgo.It("should return length of slice with one element", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			bc := byCreated{containers: []types.Container{c1}}
			gomega.Expect(bc.Len()).To(gomega.Equal(1))
		})

		ginkgo.It("should return length of slice with multiple elements", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c3 := mocks.NewMockContainer(ginkgo.GinkgoT())
			bc := byCreated{containers: []types.Container{c1, c2, c3}}
			gomega.Expect(bc.Len()).To(gomega.Equal(3))
		})
	})

	ginkgo.Describe("Swap", func() {
		ginkgo.It("should swap elements at different indices", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")
			c3 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c3.EXPECT().Name().Return("c3")

			bc := byCreated{
				containers:  []types.Container{c1, c2, c3},
				parsedTimes: []time.Time{{}, {}, {}},
			}
			bc.Swap(0, 2)
			gomega.Expect(bc.containers[0].Name()).To(gomega.Equal("c3"))
			gomega.Expect(bc.containers[2].Name()).To(gomega.Equal("c1"))
			gomega.Expect(bc.containers[1].Name()).To(gomega.Equal("c2"))
		})

		ginkgo.It("should swap adjacent elements", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2.EXPECT().Name().Return("c2")

			bc := byCreated{containers: []types.Container{c1, c2}, parsedTimes: []time.Time{{}, {}}}
			bc.Swap(0, 1)
			gomega.Expect(bc.containers[0].Name()).To(gomega.Equal("c2"))
			gomega.Expect(bc.containers[1].Name()).To(gomega.Equal("c1"))
		})

		ginkgo.It("should handle swapping same index", func() {
			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c1.EXPECT().Name().Return("c1")

			bc := byCreated{containers: []types.Container{c1}, parsedTimes: []time.Time{{}}}
			bc.Swap(0, 0)
			gomega.Expect(bc.containers[0].Name()).To(gomega.Equal("c1"))
		})
	})

	ginkgo.Describe("Less", func() {
		ginkgo.It("should return true when i is created before j", func() {
			now := time.Now()

			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())

			bc := byCreated{
				containers:  []types.Container{c1, c2},
				parsedTimes: []time.Time{now.Add(-2 * time.Hour), now.Add(-1 * time.Hour)},
			}
			gomega.Expect(bc.Less(0, 1)).To(gomega.BeTrue())
		})

		ginkgo.It("should return false when i is created after j", func() {
			now := time.Now()

			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())

			bc := byCreated{
				containers:  []types.Container{c1, c2},
				parsedTimes: []time.Time{now.Add(-1 * time.Hour), now.Add(-2 * time.Hour)},
			}
			gomega.Expect(bc.Less(0, 1)).To(gomega.BeFalse())
		})

		ginkgo.It("should return false when creation times are equal", func() {
			now := time.Now()

			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())

			bc := byCreated{
				containers:  []types.Container{c1, c2},
				parsedTimes: []time.Time{now, now},
			}
			gomega.Expect(bc.Less(0, 1)).To(gomega.BeFalse())
		})

		ginkgo.It("should handle invalid timestamp for i by using far future time", func() {
			now := time.Now()
			farFuture := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())

			bc := byCreated{
				containers:  []types.Container{c1, c2},
				parsedTimes: []time.Time{farFuture, now},
			}
			// c1 uses far future, c2 uses now, so c1 > c2, Less(0,1) false
			gomega.Expect(bc.Less(0, 1)).To(gomega.BeFalse())
		})

		ginkgo.It("should handle invalid timestamp for j by using far future time", func() {
			now := time.Now()
			farFuture := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())

			bc := byCreated{
				containers:  []types.Container{c1, c2},
				parsedTimes: []time.Time{now, farFuture},
			}
			// c1 uses now, c2 uses far future, so c1 < c2, so Less(0,1) true
			gomega.Expect(bc.Less(0, 1)).To(gomega.BeTrue())
		})

		ginkgo.It("should handle both invalid timestamps by using far future time", func() {
			farFuture := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

			c1 := mocks.NewMockContainer(ginkgo.GinkgoT())
			c2 := mocks.NewMockContainer(ginkgo.GinkgoT())

			bc := byCreated{
				containers:  []types.Container{c1, c2},
				parsedTimes: []time.Time{farFuture, farFuture},
			}
			// Both use far future, so equal, Less returns false
			gomega.Expect(bc.Less(0, 1)).To(gomega.BeFalse())
		})
	})
})
