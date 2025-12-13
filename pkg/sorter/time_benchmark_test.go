package sorter

import (
	"fmt"
	"testing"
	"time"

	dockerContainerTypes "github.com/docker/docker/api/types/container"
	dockerImageTypes "github.com/docker/docker/api/types/image"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

type simpleContainer struct {
	info *dockerContainerTypes.InspectResponse
	name string
	id   types.ContainerID
}

func (s *simpleContainer) ContainerInfo() *dockerContainerTypes.InspectResponse { return s.info }
func (s *simpleContainer) Name() string                                         { return s.name }
func (s *simpleContainer) ID() types.ContainerID                                { return s.id }
func (s *simpleContainer) IsRunning() bool                                      { return true }
func (s *simpleContainer) ImageID() types.ImageID                               { return "" }
func (s *simpleContainer) SafeImageID() types.ImageID                           { return "" }
func (s *simpleContainer) ImageName() string                                    { return "" }

func (s *simpleContainer) Enabled() (bool, bool)                   { return true, true }
func (s *simpleContainer) IsMonitorOnly(_ types.UpdateParams) bool { return false }

func (s *simpleContainer) Scope() (string, bool)                                 { return "", false }
func (s *simpleContainer) Links() []string                                       { return nil }
func (s *simpleContainer) ToRestart() bool                                       { return false }
func (s *simpleContainer) IsWatchtower() bool                                    { return false }
func (s *simpleContainer) StopSignal() string                                    { return "" }
func (s *simpleContainer) HasImageInfo() bool                                    { return false }
func (s *simpleContainer) ImageInfo() *dockerImageTypes.InspectResponse          { return nil }
func (s *simpleContainer) GetLifecyclePreCheckCommand() string                   { return "" }
func (s *simpleContainer) GetLifecyclePostCheckCommand() string                  { return "" }
func (s *simpleContainer) GetLifecyclePreUpdateCommand() string                  { return "" }
func (s *simpleContainer) GetLifecyclePostUpdateCommand() string                 { return "" }
func (s *simpleContainer) GetLifecycleUID() (int, bool)                          { return 0, false }
func (s *simpleContainer) GetLifecycleGID() (int, bool)                          { return 0, false }
func (s *simpleContainer) VerifyConfiguration() error                            { return nil }
func (s *simpleContainer) SetStale(_ bool)                                       {}
func (s *simpleContainer) IsStale() bool                                         { return false }
func (s *simpleContainer) IsNoPull(_ types.UpdateParams) bool                    { return false }
func (s *simpleContainer) SetLinkedToRestarting(_ bool)                          {}
func (s *simpleContainer) IsLinkedToRestarting() bool                            { return false }
func (s *simpleContainer) PreUpdateTimeout() int                                 { return 0 }
func (s *simpleContainer) PostUpdateTimeout() int                                { return 0 }
func (s *simpleContainer) IsRestarting() bool                                    { return false }
func (s *simpleContainer) GetCreateConfig() *dockerContainerTypes.Config         { return nil }
func (s *simpleContainer) GetCreateHostConfig() *dockerContainerTypes.HostConfig { return nil }

func BenchmarkTimeSorterSort(b *testing.B) {
	ts := TimeSorter{}
	containers := make([]types.Container, 1000)

	now := time.Now()
	for i := range containers {
		created := now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		if i%100 == 0 {
			created = "invalid-timestamp"
		}

		containers[i] = &simpleContainer{
			info: &dockerContainerTypes.InspectResponse{
				ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
					Created: created,
				},
				Config: &dockerContainerTypes.Config{},
			},
			name: fmt.Sprintf("container-%d", i),
			id:   types.ContainerID(fmt.Sprintf("id-%d", i)),
		}
	}

	b.ResetTimer()

	for range b.N {
		ts.Sort(containers)
	}
}
