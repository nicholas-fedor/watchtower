package session

import (
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// State indicates what the current state is of the container.
type State int

// State enum values.
const (
	// UnknownState is only used to represent an uninitialized State value.
	UnknownState State = iota
	SkippedState
	ScannedState
	UpdatedState
	FailedState
	FreshState
	StaleState
)

// ContainerStatus contains the container state during a session.
//
//nolint:errname // containerStatus is not an error type, it contains an error field
type ContainerStatus struct {
	containerID    types.ContainerID
	oldImage       types.ImageID
	newImage       types.ImageID
	containerName  string
	imageName      string
	containerError error
	state          State
}

// ID returns the container ID.
func (u *ContainerStatus) ID() types.ContainerID {
	return u.containerID
}

// Name returns the container name.
func (u *ContainerStatus) Name() string {
	return u.containerName
}

// CurrentImageID returns the image ID that the container used when the session started.
func (u *ContainerStatus) CurrentImageID() types.ImageID {
	return u.oldImage
}

// LatestImageID returns the newest image ID found during the session.
func (u *ContainerStatus) LatestImageID() types.ImageID {
	return u.newImage
}

// ImageName returns the name:tag that the container uses.
func (u *ContainerStatus) ImageName() string {
	return u.imageName
}

// Error returns the error (if any) that was encountered for the container during a session.
func (u *ContainerStatus) Error() string {
	if u.containerError == nil {
		return ""
	}

	return u.containerError.Error()
}

// Maps State enum values to human-readable names.
func (u *ContainerStatus) State() string {
	switch u.state {
	case UnknownState:
		return "Unknown" // Uninitialized state
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
		return "Unknown" // Handles unexpected states gracefully
	}
}
