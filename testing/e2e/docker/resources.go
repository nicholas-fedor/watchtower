package docker

import "github.com/nicholas-fedor/watchtower/testing/e2e/engine"

// ApplyEnvelope copies a calibrated CPU/memory/pids budget onto Docker HostConfig fields.
type ApplyEnvelope struct {
	// MemoryBytes is HostConfig.Memory.
	MemoryBytes int64
	// NanoCPUs is HostConfig.NanoCPUs.
	NanoCPUs int64
	// PidsLimit is HostConfig.PidsLimit.
	PidsLimit int64
}

// FromEngine converts an engine envelope into Docker host-config values.
//
// Parameters:
//   - envelope: Calibrated budget. Zero fields mean unset.
//
// Returns:
//   - ApplyEnvelope: Values to write onto HostConfig.
func FromEngine(envelope engine.Envelope) ApplyEnvelope {
	return ApplyEnvelope{
		MemoryBytes: envelope.MemoryBytes,
		NanoCPUs:    envelope.NanoCPUs,
		PidsLimit:   envelope.PidsLimit,
	}
}
