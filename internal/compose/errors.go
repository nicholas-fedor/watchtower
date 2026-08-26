package compose

import "errors"

var (
	// errNilProject indicates InjectLabels was called with a nil project.
	errNilProject = errors.New("compose project is nil")
	// errEmptyProjectDir indicates Apply was called without a project directory.
	errEmptyProjectDir = errors.New("compose project directory is empty")
	// errNoComposeServices indicates Apply was called with no service names.
	errNoComposeServices = errors.New("no compose services to apply")
	// ErrProjectDir indicates compose-dir or --compose-project pointed at an unreadable path.
	ErrProjectDir = errors.New("compose project directory is not readable")
)
