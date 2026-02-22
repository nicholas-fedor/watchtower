package sorter

import (
	"sort"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TimeSorter sorts containers by creation time.
type TimeSorter struct{}

// Sort sorts containers in place by creation time, using far future time as fallback for invalid dates.
//
// Parameters:
//   - containers: Slice to sort in place.
//
// Returns:
//   - error: Always nil (no errors possible).
func (ts TimeSorter) Sort(containers []types.Container) error {
	parsedTimes := make([]time.Time, len(containers))
	farFuture := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

	for i, c := range containers {
		createdTime, err := time.Parse(time.RFC3339Nano, c.ContainerInfo().Created)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"container_id": c.ID().ShortID(),
				"name":         c.Name(),
				"created":      c.ContainerInfo().Created,
			}).WithError(err).Debug("Failed to parse created time, using far future time as fallback")

			createdTime = farFuture
		}

		parsedTimes[i] = createdTime
	}

	sort.Sort(byCreated{containers: containers, parsedTimes: parsedTimes})

	return nil
}

// byCreated implements sort.Interface for creation time sorting.
type byCreated struct {
	containers  []types.Container
	parsedTimes []time.Time
}

// Len returns the number of containers.
//
// Returns:
//   - int: Container count.
func (c byCreated) Len() int { return len(c.containers) }

// Swap exchanges two containers by index.
//
// Parameters:
//   - i, j: Indices to swap.
func (c byCreated) Swap(i, j int) {
	c.containers[i], c.containers[j] = c.containers[j], c.containers[i]
	c.parsedTimes[i], c.parsedTimes[j] = c.parsedTimes[j], c.parsedTimes[i]
}

// Less compares creation times using pre-parsed times.
//
// Parameters:
//   - i, j: Indices to compare.
//
// Returns:
//   - bool: True if i was created before j.
func (c byCreated) Less(i, j int) bool {
	return c.parsedTimes[i].Before(c.parsedTimes[j])
}
