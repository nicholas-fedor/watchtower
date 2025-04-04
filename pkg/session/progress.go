package session

import (
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Progress contains the current session container status.
type Progress map[types.ContainerID]*ContainerStatus

// UpdateFromContainer sets various status fields from their corresponding container equivalents.
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

// AddSkipped adds a container to the Progress with the state set as skipped.
func (m Progress) AddSkipped(cont types.Container, err error) {
	update := UpdateFromContainer(cont, cont.SafeImageID(), SkippedState)
	update.containerError = err
	m.Add(update)
	logrus.WithFields(logrus.Fields{
		"container_id": cont.ID().ShortID(),
		"name":         cont.Name(),
	}).WithError(err).Debug("Added container as skipped")
}

// AddScanned adds a container to the Progress with the state set as scanned.
func (m Progress) AddScanned(cont types.Container, newImage types.ImageID) {
	m.Add(UpdateFromContainer(cont, newImage, ScannedState))
	logrus.WithFields(logrus.Fields{
		"container_id": cont.ID().ShortID(),
		"name":         cont.Name(),
		"new_image":    newImage.ShortID(),
	}).Debug("Added container as scanned")
}

// UpdateFailed updates the containers passed, setting their state as failed with the supplied error.
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

// Add a container to the map using container ID as the key.
func (m Progress) Add(update *ContainerStatus) {
	m[update.containerID] = update
	logrus.WithFields(logrus.Fields{
		"container_id": update.containerID.ShortID(),
		"name":         update.containerName,
		"state":        update.State(),
	}).Debug("Added container status to progress map")
}

// MarkForUpdate marks the container identified by containerID for update.
func (m Progress) MarkForUpdate(containerID types.ContainerID) {
	update := m[containerID]
	update.state = UpdatedState
	logrus.WithFields(logrus.Fields{
		"container_id": containerID.ShortID(),
		"name":         update.Name(),
	}).Debug("Marked container for update")
}

// Report creates a new Report from a Progress instance.
func (m Progress) Report() types.Report {
	logrus.WithFields(logrus.Fields{
		"count": len(m),
	}).Debug("Generating report from progress")

	return NewReport(m)
}
