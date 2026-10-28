// Package implicitrestart decides which containers restart because a container
// they depend on restarts.
//
// It follows the links of an update session's dependency graph from the
// restarting containers to the containers that depend on them, and returns the
// containers that restart without being updated themselves.
package implicitrestart
