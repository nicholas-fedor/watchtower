package container

import "errors"

var (
	// errCommandFailed is returned when an executed command fails with a non-zero exit code.
	errCommandFailed = errors.New("command execution failed")
	// errNoImageInfo indicates that no image information is available for a container.
	// It is returned by VerifyConfiguration when imageInfo is nil.
	errNoImageInfo = errors.New("no available image info")
	// errNoContainerInfo indicates that no container information is available.
	// It is returned by VerifyConfiguration when containerInfo is nil.
	errNoContainerInfo = errors.New("no available container info")
	// errInvalidConfig indicates an invalid or missing container configuration.
	// It is returned by VerifyConfiguration when Config or HostConfig is nil.
	errInvalidConfig       = errors.New("container configuration missing or invalid")
	errLabelNotFound       = errors.New("label was not found in container")
	errContainerNotRemoved = errors.New("container could not be removed")
	errPinnedImage         = errors.New("container uses a pinned image, and cannot be updated by watchtower")
)
