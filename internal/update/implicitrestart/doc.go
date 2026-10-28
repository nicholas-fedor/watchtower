// Package implicitrestart decides which containers restart because a container
// they depend on restarts.
//
// It follows the links of an update session's dependency graph from the
// restarting containers to the containers that depend on them. Those
// dependents restart without being updated themselves. It then returns every
// container the session restarts, both the updated ones and their dependents.
package implicitrestart
