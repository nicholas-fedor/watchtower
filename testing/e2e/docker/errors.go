package docker

import "errors"

var (
	// ErrEmptyResetPrefix means Reset was called without a case prefix.
	ErrEmptyResetPrefix = errors.New("reset prefix must not be empty")
	// ErrModuleRootNotFound means go.mod was not found walking parents.
	ErrModuleRootNotFound = errors.New("e2e go.mod not found")
	// ErrDinDExecFailed means a command inside the DinD container exited non-zero.
	ErrDinDExecFailed = errors.New("dind exec failed")
	// errUnknownGraph means Topology.Graph is not a known kind.
	errUnknownGraph = errors.New("unknown graph")
	// errDetailsMissing means /v1/containers/details omitted the subject.
	errDetailsMissing = errors.New("container missing from details")
	// errDetailsNotEnabled means details.enabled was not true.
	errDetailsNotEnabled = errors.New("details.enabled is not true")
	// errWatchtowerNoIP means the Watchtower container has no bridge address.
	errWatchtowerNoIP = errors.New("watchtower has no ip")
	// errNoContainerIP means inspect returned no IPv4 address.
	errNoContainerIP = errors.New("container has no ip")
	// errPersonaFault means the persona control API did not arm the fault.
	errPersonaFault = errors.New("set persona fault")
)
