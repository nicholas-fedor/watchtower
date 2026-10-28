// Package restart replaces the containers an update session restarts.
//
// It stops containers and recreates them from their inspect data, in
// dependency order. Each container is replaced on its own with rolling
// restarts, and otherwise every container stops before any is recreated. It
// also renames a Watchtower container out of the way of its replacement, and
// reports a container the session deliberately left untouched as skipped
// rather than failed.
package restart
