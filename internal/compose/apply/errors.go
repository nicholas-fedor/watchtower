package apply

import "errors"

// Errors for applying a Compose project.
var (
	// errNilProject indicates injectLabels was called with a nil project.
	errNilProject = errors.New("compose project is nil")
	// errNoComposeServices indicates Apply was called with no service names.
	errNoComposeServices = errors.New("no compose services to apply")
	// errEmptyProjectName indicates the loaded project has no name, so compose start cannot find its containers.
	errEmptyProjectName = errors.New("compose project name is empty")
)
