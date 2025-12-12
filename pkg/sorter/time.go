package sorter

import (
	"sort"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TimeSorter sorts containers by creation time.
type TimeSorter struct{}

// Sort sorts containers in place by creation time, using current time as fallback for invalid dates.
//
// Parameters:
//   - containers: Slice to sort in place.
//
// Returns:
//   - error: Always nil (no errors possible).
func (ts TimeSorter) Sort(containers []types.Container) error {
	sort.Sort(byCreated(containers))

	return nil
}

// byCreated implements sort.Interface for creation time sorting.
type byCreated []types.Container

// Len returns the number of containers.
//
// Returns:
//   - int: Container count.
func (c byCreated) Len() int { return len(c) }

// Swap exchanges two containers by index.
//
// Parameters:
//   - i, indexJ: Indices to swap.
func (c byCreated) Swap(i, indexJ int) { c[i], c[indexJ] = c[indexJ], c[i] }

// Less compares creation times, using now as fallback.
//
// Parameters:
//   - i, indexJ: Indices to compare.
//
// Returns:
//   - bool: True if i was created before j.
func (c byCreated) Less(i, indexJ int) bool {
	// Parse creation time for container i.
	createdTimeI, err := time.Parse(time.RFC3339Nano, c[i].ContainerInfo().Created)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"container_id": c[i].ID().ShortID(),
			"name":         c[i].Name(),
			"created":      c[i].ContainerInfo().Created,
		}).WithError(err).Debug("Failed to parse created time, using current time as fallback")

		createdTimeI = time.Now()
	}

	// Parse creation time for container j.
	createdTimeJ, err := time.Parse(time.RFC3339Nano, c[indexJ].ContainerInfo().Created)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"container_id": c[indexJ].ID().ShortID(),
			"name":         c[indexJ].Name(),
			"created":      c[indexJ].ContainerInfo().Created,
		}).WithError(err).Debug("Failed to parse created time, using current time as fallback")

		createdTimeJ = time.Now()
	}

	return createdTimeI.Before(createdTimeJ)
}
