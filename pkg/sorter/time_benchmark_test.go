package sorter

import (
	"fmt"
	"testing"
	"time"

	dockerContainerTypes "github.com/docker/docker/api/types/container"

	"github.com/nicholas-fedor/watchtower/pkg/sorter/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func BenchmarkTimeSorterSort(b *testing.B) {
	ts := TimeSorter{}
	containers := make([]types.Container, 1000)

	now := time.Now()
	for i := range containers {
		created := now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		if i%100 == 0 {
			created = "invalid-timestamp"
		}

		containers[i] = &mocks.SimpleContainer{
			ContainerInfoField: &dockerContainerTypes.InspectResponse{
				ContainerJSONBase: &dockerContainerTypes.ContainerJSONBase{
					Created: created,
				},
				Config: &dockerContainerTypes.Config{},
			},
			ContainerName: fmt.Sprintf("container-%d", i),
			ContainerID:   types.ContainerID(fmt.Sprintf("id-%d", i)),
		}
	}

	for b.Loop() {
		ts.Sort(containers)
	}
}
