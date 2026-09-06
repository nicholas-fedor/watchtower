package session

import (
	"time"

	"github.com/rs/zerolog"

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
func UpdateFromContainer(log *zerolog.Logger,
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
	log.Debug().
		Str("container_id", container.ID().ShortID()).
		Str("name", container.Name()).
		Str("state", update.State()).
		Msg("Updated container status from container")

	return update
}

// AddSkipped adds a container as skipped with an error.
//
// Parameters:
//   - container: Container to add.
//   - err: Skip reason error.
//   - params: Update parameters for monitor-only check.
func (m Progress) AddSkipped(log *zerolog.Logger, container types.Container, err error, params types.UpdateParams) {
	update := UpdateFromContainer(log, container, container.ImageID(), SkippedState, params)
	update.containerError = err
	m.Add(log, update)
	log.Debug().
		Err(err).
		Str("container_id", container.ID().ShortID()).
		Str("name", container.Name()).
		Msg("Added container as skipped")
}

// AddFailed adds a container as failed with an error.
//
// Parameters:
//   - log: Logger for diagnostics.
//   - container: Container to add.
//   - err: Failure reason error.
//   - params: Update parameters for monitor-only check.
func (m Progress) AddFailed(log *zerolog.Logger, container types.Container, err error, params types.UpdateParams) {
	update := UpdateFromContainer(log, container, container.ImageID(), FailedState, params)
	update.containerError = err
	m.Add(log, update)
	log.Debug().
		Err(err).
		Str("container_id", container.ID().ShortID()).
		Str("name", container.Name()).
		Msg("Added container as failed")
}

// AddScanned adds a container as scanned with a new image.
//
// Parameters:
//   - container: Container to add.
//   - newImage: Latest image ID.
//   - params: Update parameters for monitor-only check.
func (m Progress) AddScanned(log *zerolog.Logger,
	container types.Container,
	newImage types.ImageID,
	params types.UpdateParams,
) {
	m.Add(log, UpdateFromContainer(log, container, newImage, ScannedState, params))
	log.Debug().
		Str("container_id", container.ID().ShortID()).
		Str("name", container.Name()).
		Str("new_image", newImage.ShortID()).
		Msg("Added container as scanned")
}

// UpdateFailed marks containers as failed with errors.
//
// Parameters:
//   - failures: Map of container IDs to errors.
func (m Progress) UpdateFailed(log *zerolog.Logger, failures map[types.ContainerID]error) {
	for containerID, err := range failures {
		update, exists := m[containerID]
		if !exists {
			log.Debug().
				Str("container_id", containerID.ShortID()).
				Msg("Container not found in progress map, cannot mark as failed")

			continue
		}

		update.containerError = err
		update.state = FailedState
		log.Warn().
			Err(err).
			Str("container_id", containerID.ShortID()).
			Str("name", update.Name()).
			Msg("Updated container state to failed")
	}
}

// Add inserts a container status into the progress map.
//
// Parameters:
//   - update: Status to add.
func (m Progress) Add(log *zerolog.Logger, update *ContainerStatus) {
	m[update.containerID] = update
	log.Debug().
		Str("container_id", update.containerID.ShortID()).
		Str("name", update.containerName).
		Str("state", update.State()).
		Msg("Added container status to progress map")
}

// MarkForUpdate sets a container's state to updated.
//
// Parameters:
//   - containerID: ID of container to mark.
func (m Progress) MarkForUpdate(log *zerolog.Logger, containerID types.ContainerID) {
	update, exists := m[containerID]
	if !exists {
		log.Debug().
			Str("container_id", containerID.ShortID()).
			Msg("Attempted to mark non-existent container for update")

		return
	}

	update.state = UpdatedState
	log.Debug().
		Str("container_id", containerID.ShortID()).
		Str("name", update.Name()).
		Msg("Marked container for update")
}

// MarkRestarted sets a container's state to restarted.
//
// Parameters:
//   - containerID: ID of container to mark.
func (m Progress) MarkRestarted(log *zerolog.Logger, containerID types.ContainerID) {
	update, exists := m[containerID]
	if !exists {
		log.Debug().
			Str("container_id", containerID.ShortID()).
			Msg("Attempted to mark non-existent container as restarted")

		return
	}

	update.state = RestartedState
	log.Debug().
		Str("container_id", containerID.ShortID()).
		Str("name", update.Name()).
		Msg("Marked container as restarted")
}

// SetCooldownInfo sets cooldown metadata on a container's status.
//
// Parameters:
//   - containerID: Container ID.
//   - age: Human-readable image age.
//   - delay: Human-readable cooldown duration.
//   - remaining: Human-readable remaining time (empty if passed).
//   - eligibleAt: Time when the container becomes eligible for update.
//   - passed: True if the image passed the cooldown check.
func (m Progress) SetCooldownInfo(log *zerolog.Logger,
	containerID types.ContainerID,
	age,
	delay,
	remaining string,
	eligibleAt time.Time,
	passed bool,
) {
	update, exists := m[containerID]
	if !exists {
		log.Debug().
			Str("container_id", containerID.ShortID()).
			Msg("Attempted to set cooldown info on non-existent container")

		return
	}

	update.SetCooldownInfo(age, delay, remaining, eligibleAt, passed)
	log.Debug().
		Str("container_id", containerID.ShortID()).
		Str("name", update.Name()).
		Bool("passed", passed).
		Str("image_age", age).
		Str("cooldown", delay).
		Str("remaining", remaining).
		Msg("Set cooldown info on container")
}

// Restarted returns all containers marked as restarted.
//
// Returns:
//   - []types.ContainerReport: List of restarted containers.
func (m Progress) Restarted(log *zerolog.Logger) []types.ContainerReport {
	var restarted []types.ContainerReport

	for _, update := range m {
		if update.state == RestartedState {
			restarted = append(restarted, update)
		}
	}

	log.Debug().
		Int("count", len(restarted)).
		Msg("Retrieved restarted containers")

	return restarted
}

// Report generates a report from the progress data.
//
// Returns:
//   - types.Report: New report instance.
func (m Progress) Report(log *zerolog.Logger) types.Report {
	log.Debug().
		Int("count", len(m)).
		Msg("Generating report")

	return NewReport(log, m)
}
