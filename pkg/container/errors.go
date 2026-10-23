package container

import (
	"errors"
)

// errInspectImageFailed indicates a failure to inspect an image from the Docker daemon.
// The image client and the recreate code both report it.
var errInspectImageFailed = errors.New("failed to inspect image")
