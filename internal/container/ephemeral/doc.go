// Package ephemeral creates the short-lived orchestrator container that replaces
// a Watchtower container during its self-update.
//
// The orchestrator inherits the source container's Docker connection, so it can
// stop the old Watchtower container and start the new one.
package ephemeral
