package project

import "errors"

// Errors for resolving and loading a Compose project.
var (
	// ErrProjectDir indicates compose-dir or --compose-project pointed at an unreadable path.
	ErrProjectDir = errors.New("compose project directory is not readable")
	// ErrEmptyProjectDir indicates a project was loaded or applied without a directory.
	ErrEmptyProjectDir = errors.New("compose project directory is empty")
	// errConfigFile indicates a compose config_files entry leaves the project directory.
	errConfigFile = errors.New("compose config file is outside the project directory")
)
