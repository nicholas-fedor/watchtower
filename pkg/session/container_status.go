package session

import (
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// State enum values.
const (
	UnknownState   State = iota // Uninitialized state.
	SkippedState                // Container skipped.
	ScannedState                // Container scanned.
	UpdatedState                // Container updated.
	FailedState                 // Container update failed.
	FreshState                  // Container is fresh.
	StaleState                  // Container is stale.
	RestartedState              // Container restarted (linked dependency).
)

// State indicates what the current state is of the container.
type State int

// State string constants.
const (
	UnknownStateString   = "Unknown"
	SkippedStateString   = "Skipped"
	ScannedStateString   = "Scanned"
	UpdatedStateString   = "Updated"
	FailedStateString    = "Failed"
	FreshStateString     = "Fresh"
	StaleStateString     = "Stale"
	RestartedStateString = "Restarted"
)

// ContainerStatus holds a container’s state during a session.
//
//nolint:errname // ContainerStatus is not an error type, it contains an error field.
type ContainerStatus struct {
	containerID    types.ContainerID // Container ID.
	oldImage       types.ImageID     // Original image ID.
	newImage       types.ImageID     // Latest image ID.
	containerName  string            // Container name.
	imageName      string            // Image name with tag.
	containerError error             // Error encountered, if any.
	state          State             // Current state.
	monitorOnly    bool              // Monitor-only flag.
	newContainerID types.ContainerID // New container ID after update.
}

// ID returns the container ID.
//
// Returns:
//   - types.ContainerID: Container’s unique identifier.
func (u *ContainerStatus) ID() types.ContainerID {
	return u.containerID
}

// Name returns the container name.
//
// Returns:
//   - string: Container’s name.
func (u *ContainerStatus) Name() string {
	return u.containerName
}

// CurrentImageID returns the original image ID.
//
// Returns:
//   - types.ImageID: Image ID at session start.
func (u *ContainerStatus) CurrentImageID() types.ImageID {
	return u.oldImage
}

// LatestImageID returns the latest image ID.
//
// Returns:
//   - types.ImageID: Newest image ID from session.
func (u *ContainerStatus) LatestImageID() types.ImageID {
	return u.newImage
}

// ImageName returns the image name with tag.
//
// Returns:
//   - string: Image name (e.g., "nginx:latest").
func (u *ContainerStatus) ImageName() string {
	return u.imageName
}

// Error returns the session error, if any.
//
// Returns:
//   - string: Error message or empty if none.
func (u *ContainerStatus) Error() string {
	if u.containerError == nil {
		return ""
	}

	return u.containerError.Error()
}

// State returns the human-readable state name.
//
// Returns:
//   - string: State as a string (e.g., "Updated").
func (u *ContainerStatus) State() string {
	switch u.state {
	case UnknownState:
		return UnknownStateString // Uninitialized state.
	case SkippedState:
		return SkippedStateString
	case ScannedState:
		return ScannedStateString
	case UpdatedState:
		return UpdatedStateString
	case FailedState:
		return FailedStateString
	case FreshState:
		return FreshStateString
	case StaleState:
		return StaleStateString
	case RestartedState:
		return RestartedStateString
	default:
		return UnknownStateString // Fallback for unexpected values.
	}
}

// IsMonitorOnly returns whether the container is in monitor-only mode.
//
// Returns:
//   - bool: True if monitor-only, false otherwise.
func (u *ContainerStatus) IsMonitorOnly() bool {
	return u.monitorOnly
}

// NewContainerID returns the new container ID after update.
//
// Returns:
//   - types.ContainerID: New container ID or empty if not updated.
func (u *ContainerStatus) NewContainerID() types.ContainerID {
	return u.newContainerID
}

// SetNewContainerID sets the new container ID after update.
//
// Parameters:
//   - newID: The new container ID.
func (u *ContainerStatus) SetNewContainerID(newID types.ContainerID) {
	u.newContainerID = newID
}
