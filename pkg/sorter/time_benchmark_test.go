package sorter

import (
	"fmt"
	"testing"
	"time"

	dockerContainer "github.com/docker/docker/api/types/container"

	mockSorter "github.com/nicholas-fedor/watchtower/pkg/sorter/mocks"
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

		containers[i] = &mockSorter.SimpleContainer{
			ContainerInfoField: &dockerContainer.InspectResponse{
				ContainerJSONBase: &dockerContainer.ContainerJSONBase{
					Created: created,
				},
				Config: &dockerContainer.Config{},
			},
			ContainerName: fmt.Sprintf("container-%d", i),
			ContainerID:   types.ContainerID(fmt.Sprintf("id-%d", i)),
		}
	}

	b.ResetTimer()

	for b.Loop() {
		temp := make([]types.Container, len(containers))
		copy(temp, containers)
		ts.Sort(temp)
	}
}
