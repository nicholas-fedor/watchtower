package session

import (
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Progress tracks container statuses during a session.
type Progress map[types.ContainerID]*ContainerStatus

// UpdateFromContainer creates a status from container data.
//
// Parameters:
//   - cont: Container to update from.
//   - newImage: Latest image ID.
//   - state: Container state.
//
// Returns:
//   - *ContainerStatus: Updated status.
func UpdateFromContainer(
	cont types.Container,
	newImage types.ImageID,
	state State,
) *ContainerStatus {
	update := &ContainerStatus{
		containerID:    cont.ID(),
		oldImage:       cont.SafeImageID(),
		newImage:       newImage,
		containerName:  cont.Name(),
		imageName:      cont.ImageName(),
		containerError: nil,
		state:          state,
	}
	logrus.WithFields(logrus.Fields{
		"container_id": cont.ID().ShortID(),
		"name":         cont.Name(),
		"state":        update.State(),
	}).Debug("Updated container status from container")

	return update
}

// AddSkipped adds a container as skipped with an error.
//
// Parameters:
//   - cont: Container to add.
//   - err: Skip reason error.
func (m Progress) AddSkipped(cont types.Container, err error) {
	update := UpdateFromContainer(cont, cont.SafeImageID(), SkippedState)
	update.containerError = err
	m.Add(update)
	logrus.WithFields(logrus.Fields{
		"container_id": cont.ID().ShortID(),
		"name":         cont.Name(),
	}).WithError(err).Debug("Added container as skipped")
}

// AddScanned adds a container as scanned with a new image.
//
// Parameters:
//   - cont: Container to add.
//   - newImage: Latest image ID.
func (m Progress) AddScanned(cont types.Container, newImage types.ImageID) {
	m.Add(UpdateFromContainer(cont, newImage, ScannedState))
	logrus.WithFields(logrus.Fields{
		"container_id": cont.ID().ShortID(),
		"name":         cont.Name(),
		"new_image":    newImage.ShortID(),
	}).Debug("Added container as scanned")
}

// UpdateFailed marks containers as failed with errors.
//
// Parameters:
//   - failures: Map of container IDs to errors.
func (m Progress) UpdateFailed(failures map[types.ContainerID]error) {
	for id, err := range failures {
		update := m[id]
		update.containerError = err
		update.state = FailedState
		logrus.WithFields(logrus.Fields{
			"container_id": id.ShortID(),
			"name":         update.Name(),
		}).WithError(err).Debug("Updated container state to failed")
	}
}

// Add inserts a container status into the progress map.
//
// Parameters:
//   - update: Status to add.
func (m Progress) Add(update *ContainerStatus) {
	m[update.containerID] = update
	logrus.WithFields(logrus.Fields{
		"container_id": update.containerID.ShortID(),
		"name":         update.containerName,
		"state":        update.State(),
	}).Debug("Added container status to progress map")
}

// MarkForUpdate sets a container’s state to updated.
//
// Parameters:
//   - containerID: ID of container to mark.
func (m Progress) MarkForUpdate(containerID types.ContainerID) {
	update := m[containerID]
	update.state = UpdatedState
	logrus.WithFields(logrus.Fields{
		"container_id": containerID.ShortID(),
		"name":         update.Name(),
	}).Debug("Marked container for update")
}

// Report generates a report from the progress data.
//
// Returns:
//   - types.Report: New report instance.
func (m Progress) Report() types.Report {
	logrus.WithField("count", len(m)).Debug("Generating report")

	return NewReport(m)
}
