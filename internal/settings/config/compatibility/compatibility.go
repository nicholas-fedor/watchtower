// Package compatibility holds runtime compatibility settings (for example Podman).
package compatibility

// Compatibility holds container recreate compatibility options.
type Compatibility struct {
	// DisableMemorySwappiness clears memory swappiness when recreating containers
	// for Podman compatibility (--disable-memory-swappiness /
	// WATCHTOWER_DISABLE_MEMORY_SWAPPINESS).
	DisableMemorySwappiness bool
	// CPUCopyMode controls CPU settings when recreating containers
	// (--cpu-copy-mode / WATCHTOWER_CPU_COPY_MODE: auto, full, none).
	CPUCopyMode string
}
