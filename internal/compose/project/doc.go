// Package project resolves and loads the Docker Compose project of a container.
//
// A project is resolved only when a container's compose-dir label or the
// compose-project configuration option names one. It is parsed with compose-go
// from its config files, or from the default Compose files in its directory,
// and config files that lead outside the project directory are rejected.
package project
