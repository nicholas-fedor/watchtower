// Package restart replaces the containers an update session restarts.
//
// It stops containers and recreates them from their inspect data, in
// dependency order. Each container is replaced on its own with rolling
// restarts, and otherwise every container stops before any is recreated. A
// Watchtower container is replaced through a self-update. A container the
// session deliberately left untouched is reported as skipped rather than
// failed.
package restart
