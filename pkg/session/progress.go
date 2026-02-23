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
//   - container: Container to update from.
//   - newImage: Latest image ID.
//   - state: Container state.
//   - params: Update parameters for monitor-only check.
//
// Returns:
//   - *ContainerStatus: Updated status.
func UpdateFromContainer(
	container types.Container,
	newImage types.ImageID,
	state State,
	params types.UpdateParams,
) *ContainerStatus {
	update := &ContainerStatus{
		containerID:    container.ID(),
		oldImage:       container.ImageID(),
		newImage:       newImage,
		containerName:  container.Name(),
		imageName:      container.ImageName(),
		containerError: nil,
		state:          state,
		monitorOnly:    container.IsMonitorOnly(params),
		newContainerID: "",
	}
	logrus.WithFields(logrus.Fields{
		"container_id": container.ID().ShortID(),
		"name":         container.Name(),
		"state":        update.State(),
	}).Debug("Updated container status from container")

	return update
}

// AddSkipped adds a container as skipped with an error.
//
// Parameters:
//   - container: Container to add.
//   - err: Skip reason error.
//   - params: Update parameters for monitor-only check.
func (m Progress) AddSkipped(container types.Container, err error, params types.UpdateParams) {
	update := UpdateFromContainer(container, container.ImageID(), SkippedState, params)
	update.containerError = err
	m.Add(update)
	logrus.WithFields(logrus.Fields{
		"container_id": container.ID().ShortID(),
		"name":         container.Name(),
	}).WithError(err).Debug("Added container as skipped")
}

// AddScanned adds a container as scanned with a new image.
//
// Parameters:
//   - container: Container to add.
//   - newImage: Latest image ID.
//   - params: Update parameters for monitor-only check.
func (m Progress) AddScanned(
	container types.Container,
	newImage types.ImageID,
	params types.UpdateParams,
) {
	m.Add(UpdateFromContainer(container, newImage, ScannedState, params))
	logrus.WithFields(logrus.Fields{
		"container_id": container.ID().ShortID(),
		"name":         container.Name(),
		"new_image":    newImage.ShortID(),
	}).Debug("Added container as scanned")
}

// UpdateFailed marks containers as failed with errors.
//
// Parameters:
//   - failures: Map of container IDs to errors.
func (m Progress) UpdateFailed(failures map[types.ContainerID]error) {
	for containerID, err := range failures {
		update, exists := m[containerID]
		if !exists {
			logrus.WithField("container_id", containerID.ShortID()).
				Debug("Container not found in progress map, cannot mark as failed")

			continue
		}

		update.containerError = err
		update.state = FailedState
		logrus.WithFields(logrus.Fields{
			"container_id": containerID.ShortID(),
			"name":         update.Name(),
		}).WithError(err).Warn("Updated container state to failed")
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
	update, exists := m[containerID]
	if !exists {
		logrus.WithField("container_id", containerID.ShortID()).
			Debug("Attempted to mark non-existent container for update")

		return
	}

	update.state = UpdatedState
	logrus.WithFields(logrus.Fields{
		"container_id": containerID.ShortID(),
		"name":         update.Name(),
	}).Debug("Marked container for update")
}

// MarkRestarted sets a container’s state to restarted.
//
// Parameters:
//   - containerID: ID of container to mark.
func (m Progress) MarkRestarted(containerID types.ContainerID) {
	update, exists := m[containerID]
	if !exists {
		logrus.WithField("container_id", containerID.ShortID()).
			Debug("Attempted to mark non-existent container as restarted")

		return
	}

	update.state = RestartedState
	logrus.WithFields(logrus.Fields{
		"container_id": containerID.ShortID(),
		"name":         update.Name(),
	}).Debug("Marked container as restarted")
}

// Restarted returns all containers marked as restarted.
//
// Returns:
//   - []types.ContainerReport: List of restarted containers.
func (m Progress) Restarted() []types.ContainerReport {
	var restarted []types.ContainerReport

	for _, update := range m {
		if update.state == RestartedState {
			restarted = append(restarted, update)
		}
	}

	logrus.WithField("count", len(restarted)).Debug("Retrieved restarted containers")

	return restarted
}

// Report generates a report from the progress data.
//
// Returns:
//   - types.Report: New report instance.
func (m Progress) Report() types.Report {
	logrus.WithField("count", len(m)).Debug("Generating report")

	return NewReport(m)
}
