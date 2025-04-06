package types

import (
	"strings"

	dockerContainerTypes "github.com/docker/docker/api/types/container"
	dockerImageTypes "github.com/docker/docker/api/types/image"
)

// Container defines a docker container’s interface in Watchtower.
type Container interface {
	ContainerInfo() *dockerContainerTypes.InspectResponse  // Container metadata.
	ID() ContainerID                                       // Container ID.
	IsRunning() bool                                       // Check if running.
	Name() string                                          // Container name.
	ImageID() ImageID                                      // Current image ID.
	SafeImageID() ImageID                                  // Current image ID or empty if nil.
	ImageName() string                                     // Image name with tag.
	Enabled() (bool, bool)                                 // Enabled status and presence.
	IsMonitorOnly(params UpdateParams) bool                // Monitor-only check.
	Scope() (string, bool)                                 // Scope value and presence.
	Links() []string                                       // Dependency links.
	ToRestart() bool                                       // Needs restart check.
	IsWatchtower() bool                                    // Watchtower instance check.
	StopSignal() string                                    // Custom stop signal.
	HasImageInfo() bool                                    // Image metadata presence.
	ImageInfo() *dockerImageTypes.InspectResponse          // Image metadata.
	GetLifecyclePreCheckCommand() string                   // Pre-check command.
	GetLifecyclePostCheckCommand() string                  // Post-check command.
	GetLifecyclePreUpdateCommand() string                  // Pre-update command.
	GetLifecyclePostUpdateCommand() string                 // Post-update command.
	VerifyConfiguration() error                            // Config validation.
	SetStale(status bool)                                  // Set stale status.
	IsStale() bool                                         // Stale status check.
	IsNoPull(params UpdateParams) bool                     // No-pull check.
	SetLinkedToRestarting(status bool)                     // Set linked-to-restarting status.
	IsLinkedToRestarting() bool                            // Linked-to-restarting check.
	PreUpdateTimeout() int                                 // Pre-update timeout.
	PostUpdateTimeout() int                                // Post-update timeout.
	IsRestarting() bool                                    // Restarting status check.
	GetCreateConfig() *dockerContainerTypes.Config         // Creation config.
	GetCreateHostConfig() *dockerContainerTypes.HostConfig // Host creation config.
}

// ImageID is a hash string for a container image.
type ImageID string

// ContainerID is a hash string for a container instance.
type ContainerID string

// ShortID returns the 12-character short version of an image ID.
//
// Returns:
//   - string: Shortened ID without "sha256:" prefix.
func (id ImageID) ShortID() string {
	return shortID(string(id))
}

// ShortID returns the 12-character short version of a container ID.
//
// Returns:
//   - string: Shortened ID without "sha256:" prefix.
func (id ContainerID) ShortID() string {
	return shortID(string(id))
}

// shortID shortens a hash string to 12 characters.
//
// Parameters:
//   - longID: Full hash string.
//
// Returns:
//   - string: Shortened ID, adjusted for "sha256:" prefix.
func shortID(longID string) string {
	prefixSep := strings.IndexRune(longID, ':')
	offset := 0
	length := 12

	// Adjust offset for "sha256:" prefix.
	if prefixSep >= 0 {
		if longID[0:prefixSep] == "sha256" {
			offset = prefixSep + 1
		} else {
			length += prefixSep + 1
		}
	}

	// Return shortened ID or full string if too short.
	if len(longID) >= offset+length {
		return longID[offset : offset+length]
	}

	return longID
}
