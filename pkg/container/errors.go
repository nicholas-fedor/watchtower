package container

import (
	"errors"
)

// Errors for container ID retrieval operations in cgroup_id.go.
var (
	// errNoValidContainerID indicates no valid Docker container ID was found in the cgroup data.
	errNoValidContainerID = errors.New("no valid docker container ID found in input")
	// errReadCgroupFile indicates a failure to read the cgroup file for the current process.
	errReadCgroupFile = errors.New("failed to read cgroup file")
	// errExtractContainerID indicates a failure to extract a container ID from the cgroup data.
	errExtractContainerID = errors.New("failed to extract container ID")
)

// Errors for client operations in client.go.
var (
	// errCreateExecFailed indicates a failure to create an exec instance in a container.
	errCreateExecFailed = errors.New("failed to create exec instance")
	// errStartExecFailed indicates a failure to start an exec instance in a container.
	errStartExecFailed = errors.New("failed to start exec instance")
	// errAttachExecFailed indicates a failure to attach to an exec instance for output capture.
	errAttachExecFailed = errors.New("failed to attach to exec instance")
	// errReadExecOutputFailed indicates a failure to read output from an exec instance.
	errReadExecOutputFailed = errors.New("failed to read exec output")
	// errInspectExecFailed indicates a failure to inspect an exec instance’s status.
	errInspectExecFailed = errors.New("failed to inspect exec instance")
	// errCommandFailed indicates a command executed in a container failed with a non-zero exit code.
	errCommandFailed = errors.New("command execution failed")
)

// Errors for container operations in container_source.go.
var (
	// errListContainersFailed indicates a failure to list containers from the Docker host.
	errListContainersFailed = errors.New("failed to list containers")
	// errInspectContainerFailed indicates a failure to inspect a container’s details.
	errInspectContainerFailed = errors.New("failed to inspect container")
	// errStopContainerFailed indicates a failure to stop a container with a signal.
	errStopContainerFailed = errors.New("failed to stop container")
	// errRemoveContainerFailed indicates a failure to remove a container from the host.
	errRemoveContainerFailed = errors.New("failed to remove container")
	// errContainerNotRemoved indicates a container was not removed after the stop operation.
	errContainerNotRemoved = errors.New("container not removed after timeout")
	// errUnexpectedMacInLegacy indicates a MAC address was found in a legacy API configuration where it should not be.
	errUnexpectedMacInLegacy = errors.New("unexpected MAC address in legacy config")
	// errUnexpectedMacInHost indicates a MAC address was found in a host network configuration where it should not be.
	errUnexpectedMacInHost = errors.New("unexpected MAC address in host network config")
	// errNoMacInNonHost indicates no MAC address was found in a non-host network configuration where one is expected.
	errNoMacInNonHost = errors.New("no MAC address found in non-host network config")
)

// Errors for container start and rename operations in container_target.go.
var (
	// errCreateContainerFailed indicates a failure to create a new container.
	errCreateContainerFailed = errors.New("failed to create container")
	// errStartContainerFailed indicates a failure to start a newly created container.
	errStartContainerFailed = errors.New("failed to start container")
	// errRenameContainerFailed indicates a failure to rename an existing container.
	errRenameContainerFailed = errors.New("failed to rename container")
)

// Errors for container configuration and metadata operations in container.go.
var (
	// errNoImageInfo indicates the container lacks image metadata required for recreation.
	errNoImageInfo = errors.New("no image info available")
	// errNoContainerInfo indicates the container lacks metadata required for recreation.
	errNoContainerInfo = errors.New("no container info available")
	// errInvalidConfig indicates the container’s configuration is invalid for recreation.
	errInvalidConfig = errors.New("invalid container configuration")
)

// Errors for image operations in image.go.
var (
	// errPinnedImage indicates an attempt to pull an immutable (sha256-pinned) image.
	errPinnedImage = errors.New("image is pinned with sha256, skipping pull")
	// errInspectImageFailed indicates a failure to inspect an image from the Docker daemon.
	errInspectImageFailed = errors.New("failed to inspect image")
	// errPullImageFailed indicates a failure to pull an image from the registry.
	errPullImageFailed = errors.New("failed to pull image")
	// errReadPullResponseFailed indicates a failure to read the pull response stream.
	errReadPullResponseFailed = errors.New("failed to read pull response")
	// errRemoveImageFailed indicates a failure to remove an image from the Docker host.
	errRemoveImageFailed = errors.New("failed to remove image")
)

// Errors for label operations in metadata.go.
var (
	// errLabelNotFound indicates a requested label is not present in the container’s metadata.
	errLabelNotFound = errors.New("label not found")
)
