package selfupdate

import (
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Block is why a self-update cannot replace the running Watchtower container.
type Block int

// Reasons that block a self-update.
const (
	// NotBlocked means a self-update may replace the running container.
	NotBlocked Block = iota
	// BlockedUninspected means the container was detected but could not be
	// inspected, so its published ports are unknown.
	BlockedUninspected
	// BlockedByPublishedPorts means the container publishes host ports, which
	// it keeps until it stops, so a replacement made by renaming it cannot bind
	// them.
	BlockedByPublishedPorts
)

// Blocked reports why a self-update cannot replace the detected Watchtower
// container safely, or NotBlocked when it can.
//
// Published host ports block a self-update made by renaming the container, but
// not an ephemeral one, whose orchestrator stops the container first. A
// container that was detected but could not be inspected blocks every
// self-update, because its ports are unknown.
//
// Without a detected container ID there is no container of its own to check,
// so it reports NotBlocked. A Watchtower that runs in a container whose ID
// could not be detected falls under a broader rule. It must leave every
// Watchtower container alone in every mode, since it cannot tell its own
// container from other instances.
//
// Parameters:
//   - current: The running Watchtower container, or nil when it was not inspected.
//   - currentID: The detected ID of the running Watchtower container, or empty.
//   - ephemeral: Whether self-updates use an ephemeral orchestrator.
//
// Returns:
//   - Block: Why a self-update is blocked, or NotBlocked.
func Blocked(current types.Container, currentID types.ContainerID, ephemeral bool) Block {
	switch {
	case current == nil && currentID != "":
		return BlockedUninspected
	case current != nil && current.HasExposedPorts() && !ephemeral:
		return BlockedByPublishedPorts
	default:
		return NotBlocked
	}
}
