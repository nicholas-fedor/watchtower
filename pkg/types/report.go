package types

// Report defines container session results.
type Report interface {
	Scanned() []ContainerReport // Scanned containers.
	Updated() []ContainerReport // Updated containers.
	Failed() []ContainerReport  // Failed containers.
	Skipped() []ContainerReport // Skipped containers.
	Stale() []ContainerReport   // Stale containers.
	Fresh() []ContainerReport   // Fresh containers.
	All() []ContainerReport     // All unique containers.
}

// ContainerReport defines a container’s session status.
type ContainerReport interface {
	ID() ContainerID         // Container ID.
	Name() string            // Container name.
	CurrentImageID() ImageID // Original image ID.
	LatestImageID() ImageID  // Latest image ID.
	ImageName() string       // Image name with tag.
	Error() string           // Error message, if any.
	State() string           // Human-readable state.
}
