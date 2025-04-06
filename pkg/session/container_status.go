package session

import (
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// State enum values.
const (
	UnknownState State = iota // Uninitialized state.
	SkippedState              // Container skipped.
	ScannedState              // Container scanned.
	UpdatedState              // Container updated.
	FailedState               // Container update failed.
	FreshState                // Container is fresh.
	StaleState                // Container is stale.
)

// State indicates what the current state is of the container.
type State int

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
		return "Unknown" // Uninitialized state.
	case SkippedState:
		return "Skipped"
	case ScannedState:
		return "Scanned"
	case UpdatedState:
		return "Updated"
	case FailedState:
		return "Failed"
	case FreshState:
		return "Fresh"
	case StaleState:
		return "Stale"
	default:
		return "Unknown" // Fallback for unexpected values.
	}
}
