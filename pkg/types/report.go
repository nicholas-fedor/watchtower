package types

// Report defines container session results.
type Report interface {
	Scanned() []ContainerReport   // Scanned containers.
	Updated() []ContainerReport   // Updated containers.
	Failed() []ContainerReport    // Failed containers.
	Skipped() []ContainerReport   // Skipped containers.
	Stale() []ContainerReport     // Stale containers.
	Fresh() []ContainerReport     // Fresh containers.
	Restarted() []ContainerReport // Restarted containers (linked dependencies).
	All() []ContainerReport       // All unique containers.
}

// ContainerReport defines a container's session status.
type ContainerReport interface {
	ID() ContainerID             // Container ID.
	Name() string                // Container name.
	CurrentImageID() ImageID     // Original image ID.
	LatestImageID() ImageID      // Latest image ID.
	ImageName() string           // Image name with tag.
	Error() string               // Error message, if any.
	State() string               // Human-readable state.
	IsMonitorOnly() bool         // Monitor-only status.
	NewContainerID() ContainerID // New container ID after update.
	GitRepo() string             // Resolved Git repository URL.
	GitRef() string              // Resolved Git ref.
	Changelog() string           // Changelog or releases URL.
	Source() string              // OCI org.opencontainers.image.source.
	ImageURL() string            // OCI org.opencontainers.image.url.
	Documentation() string       // OCI org.opencontainers.image.documentation.
	Revision() string            // OCI org.opencontainers.image.revision.
}
