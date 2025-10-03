package sorter_test

import (
	"sort"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	dockerContainerTypes "github.com/docker/docker/api/types/container"
	dockerImageTypes "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/pkg/sorter"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// mockContainer implements types.Container for testing sorting.
type mockContainer struct {
	name         string
	created      string
	links        []string
	isWatchtower bool
}

func (m *mockContainer) ContainerInfo() *dockerContainerTypes.InspectResponse {
	return &dockerContainerTypes.InspectResponse{
		ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
			Name:    m.name,
			Created: m.created,
		},
	}
}

func (m *mockContainer) Name() string    { return m.name }
func (m *mockContainer) Links() []string { return m.links }
func (m *mockContainer) ID() types.ContainerID {
	return types.ContainerID("id-" + m.name)
}
func (m *mockContainer) IsRunning() bool                              { return true }
func (m *mockContainer) IsRestarting() bool                           { return false }
func (m *mockContainer) ImageID() types.ImageID                       { return "" }
func (m *mockContainer) SafeImageID() types.ImageID                   { return "" }
func (m *mockContainer) ImageName() string                            { return "mock/image" }
func (m *mockContainer) Enabled() (bool, bool)                        { return true, true }
func (m *mockContainer) IsMonitorOnly(types.UpdateParams) bool        { return false }
func (m *mockContainer) Scope() (string, bool)                        { return "", false }
func (m *mockContainer) ToRestart() bool                              { return false }
func (m *mockContainer) IsWatchtower() bool                           { return m.isWatchtower }
func (m *mockContainer) StopSignal() string                           { return "SIGTERM" }
func (m *mockContainer) HasImageInfo() bool                           { return false }
func (m *mockContainer) ImageInfo() *dockerImageTypes.InspectResponse { return nil }
func (m *mockContainer) GetLifecyclePreCheckCommand() string          { return "" }
func (m *mockContainer) GetLifecyclePostCheckCommand() string         { return "" }
func (m *mockContainer) GetLifecyclePreUpdateCommand() string         { return "" }
func (m *mockContainer) GetLifecyclePostUpdateCommand() string        { return "" }
func (m *mockContainer) VerifyConfiguration() error                   { return nil }
func (m *mockContainer) SetStale(bool)                                {}
func (m *mockContainer) IsStale() bool                                { return false }
func (m *mockContainer) IsNoPull(types.UpdateParams) bool             { return false }
func (m *mockContainer) SetLinkedToRestarting(bool)                   {}
func (m *mockContainer) IsLinkedToRestarting() bool                   { return false }
func (m *mockContainer) PreUpdateTimeout() int                        { return 0 }
func (m *mockContainer) PostUpdateTimeout() int                       { return 0 }
func (m *mockContainer) GetLifecycleUID() (int, bool)                 { return 0, false }
func (m *mockContainer) GetLifecycleGID() (int, bool)                 { return 0, false }

func (m *mockContainer) GetCreateConfig() *dockerContainerTypes.Config {
	return &dockerContainerTypes.Config{}
}

func (m *mockContainer) GetCreateHostConfig() *dockerContainerTypes.HostConfig {
	return &dockerContainerTypes.HostConfig{}
}

var _ = ginkgo.Describe("Container Sorting", func() {
	ginkgo.Describe("ByCreated", func() {
		ginkgo.When("sorting by creation date", func() {
			ginkgo.It("sorts containers in ascending order", func() {
				now := time.Now()
				containers := []types.Container{
					&mockContainer{
						name:    "c3",
						created: now.Add(-1 * time.Hour).Format(time.RFC3339Nano),
					},
					&mockContainer{
						name:    "c1",
						created: now.Add(-3 * time.Hour).Format(time.RFC3339Nano),
					},
					&mockContainer{
						name:    "c2",
						created: now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
					},
				}
				sort.Sort(sorter.ByCreated(containers))
				gomega.Expect(containers[0].Name()).To(gomega.Equal("c1"))
				gomega.Expect(containers[1].Name()).To(gomega.Equal("c2"))
				gomega.Expect(containers[2].Name()).To(gomega.Equal("c3"))
			})

			ginkgo.It("handles invalid creation dates gracefully", func() {
				now := time.Now()
				containers := []types.Container{
					&mockContainer{name: "c1", created: "invalid-date"},
					&mockContainer{name: "c2", created: now.Format(time.RFC3339Nano)},
				}
				sort.Sort(sorter.ByCreated(containers))
				// Invalid date uses current time, order may vary; check stability
				gomega.Expect(containers).To(gomega.HaveLen(2))
			})

			ginkgo.It("handles empty list", func() {
				containers := []types.Container{}
				sort.Sort(sorter.ByCreated(containers))
				gomega.Expect(containers).To(gomega.BeEmpty())
			})
		})
	})

	ginkgo.Describe("SortByDependencies", func() {
		ginkgo.When("sorting by dependencies", func() {
			ginkgo.It("sorts containers with no links first", func() {
				containers := []types.Container{
					&mockContainer{name: "c1", links: []string{"c2"}},
					&mockContainer{name: "c2", links: nil},
				}
				sorted, err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(sorted).To(gomega.HaveLen(2))
				gomega.Expect(sorted[0].Name()).To(gomega.Equal("c2")) // No links
				gomega.Expect(sorted[1].Name()).To(gomega.Equal("c1")) // Depends on c2
			})

			ginkgo.It("handles multiple dependencies", func() {
				containers := []types.Container{
					&mockContainer{name: "c1", links: []string{"c2", "c3"}},
					&mockContainer{name: "c2", links: []string{"c3"}},
					&mockContainer{name: "c3", links: nil},
				}
				sorted, err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(sorted).To(gomega.HaveLen(3))
				gomega.Expect(sorted[0].Name()).To(gomega.Equal("c3")) // No links
				gomega.Expect(sorted[1].Name()).To(gomega.Equal("c2")) // Links to c3
				gomega.Expect(sorted[2].Name()).To(gomega.Equal("c1")) // Links to c2, c3
			})

			ginkgo.It("detects circular references", func() {
				containers := []types.Container{
					&mockContainer{name: "c1", links: []string{"c2"}},
					&mockContainer{name: "c2", links: []string{"c1"}},
				}
				_, err := sorter.SortByDependencies(containers)
				gomega.Expect(err).To(gomega.MatchError("circular reference detected: c1"))
			})

			ginkgo.It("handles missing dependencies gracefully", func() {
				containers := []types.Container{
					&mockContainer{name: "c1", links: []string{"c2"}},
					&mockContainer{name: "c3", links: nil},
				}
				sorted, err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(sorted).To(gomega.HaveLen(2))
				gomega.Expect(sorted[0].Name()).To(gomega.Equal("c3")) // No links
				gomega.Expect(sorted[1].Name()).To(gomega.Equal("c1")) // Links to missing c2
			})

			ginkgo.It("handles empty list", func() {
				containers := []types.Container{}
				sorted, err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(sorted).To(gomega.BeEmpty())
			})

			ginkgo.It("places Watchtower containers last", func() {
				containers := []types.Container{
					&mockContainer{name: "watchtower", isWatchtower: true},
					&mockContainer{name: "c1", links: []string{"c2"}},
					&mockContainer{name: "c2", links: nil},
				}
				sorted, err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(sorted).To(gomega.HaveLen(3))
				gomega.Expect(sorted[0].Name()).To(gomega.Equal("c2"))         // No links
				gomega.Expect(sorted[1].Name()).To(gomega.Equal("c1"))         // Depends on c2
				gomega.Expect(sorted[2].Name()).To(gomega.Equal("watchtower")) // Watchtower last
			})

			ginkgo.It("places multiple Watchtower containers last", func() {
				containers := []types.Container{
					&mockContainer{name: "watchtower1", isWatchtower: true},
					&mockContainer{name: "c1", links: []string{"c2"}},
					&mockContainer{name: "watchtower2", isWatchtower: true},
					&mockContainer{name: "c2", links: nil},
				}
				sorted, err := sorter.SortByDependencies(containers)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
				gomega.Expect(sorted).To(gomega.HaveLen(4))
				gomega.Expect(sorted[0].Name()).To(gomega.Equal("c2")) // No links
				gomega.Expect(sorted[1].Name()).To(gomega.Equal("c1")) // Depends on c2
				// Watchtower containers at the end (order may vary)
				watchtowerNames := []string{sorted[2].Name(), sorted[3].Name()}
				gomega.Expect(watchtowerNames).To(gomega.ContainElement("watchtower1"))
				gomega.Expect(watchtowerNames).To(gomega.ContainElement("watchtower2"))
			})
		})
	})
})

func TestSorter(t *testing.T) {
	t.Parallel()
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Sorter Suite")
}
