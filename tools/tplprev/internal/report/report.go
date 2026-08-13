package report

// Report defines container session results.
type Report interface {
	Scanned() []ContainerReport
	Updated() []ContainerReport
	Failed() []ContainerReport
	Skipped() []ContainerReport
	Stale() []ContainerReport
	Fresh() []ContainerReport
	Restarted() []ContainerReport
	All() []ContainerReport
}

// ContainerReport defines a container's session status.
type ContainerReport interface {
	ID() ContainerID
	Name() string
	CurrentImageID() ImageID
	LatestImageID() ImageID
	ImageName() string
	Error() string
	State() string
	IsMonitorOnly() bool
	NewContainerID() ContainerID
}
