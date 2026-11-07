package state

import (
	"strconv"
	"testing"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func BenchmarkReport_All(b *testing.B) {
	const n = 64

	scanned := make([]types.ContainerReport, n)
	updated := make([]types.ContainerReport, n/8)
	fresh := make([]types.ContainerReport, n/2)

	for i := range scanned {
		scanned[i] = &ContainerStatus{containerID: types.ContainerID("c" + strconv.Itoa(i))}
	}

	for i := range updated {
		updated[i] = scanned[i]
	}

	for i := range fresh {
		fresh[i] = scanned[n/4+i]
	}

	b.Run("reuseRestarted", func(b *testing.B) {
		restarted := make([]types.ContainerReport, n/8)
		for i := range restarted {
			restarted[i] = scanned[n/8+i]
		}

		b.ReportAllocs()

		for b.Loop() {
			_ = (&Report{scanned: scanned, updated: updated, restarted: restarted, fresh: fresh}).All()
		}
	})

	b.Run("uniqueRestarted", func(b *testing.B) {
		// Unique restarted IDs make the combined result larger than the old
		// capacity of scanned+updated+fresh (64+8+32).
		restarted := make([]types.ContainerReport, n)
		for i := range restarted {
			restarted[i] = &ContainerStatus{containerID: types.ContainerID("r" + strconv.Itoa(i))}
		}

		b.ReportAllocs()

		for b.Loop() {
			_ = (&Report{scanned: scanned, updated: updated, restarted: restarted, fresh: fresh}).All()
		}
	})
}
