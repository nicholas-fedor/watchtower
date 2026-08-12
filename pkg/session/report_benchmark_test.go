package session

import (
	"strconv"
	"testing"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func BenchmarkAllFromSlices(b *testing.B) {
	const n = 64

	scanned := make([]types.ContainerReport, n)
	updated := make([]types.ContainerReport, n/8)
	restarted := make([]types.ContainerReport, n/8)
	fresh := make([]types.ContainerReport, n/2)

	for i := range scanned {
		scanned[i] = &ContainerStatus{containerID: types.ContainerID("c" + strconv.Itoa(i))}
	}

	for i := range updated {
		updated[i] = scanned[i]
	}

	for i := range restarted {
		restarted[i] = scanned[n/8+i]
	}

	for i := range fresh {
		fresh[i] = scanned[n/4+i]
	}

	b.ReportAllocs()

	for b.Loop() {
		_ = allFromSlices(scanned, updated, restarted, nil, nil, nil, fresh)
	}
}
