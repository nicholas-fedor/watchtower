// Package selfupdate replaces Watchtower's own container.
//
// By default the running Watchtower renames its container out of the way, so
// the replacement can take the original name while this process still runs.
// The opt-in alternative is a short-lived orchestrator container started from
// the new image. The orchestrator stops the old container and starts its
// replacement, then removes the old container. The package also starts a
// replacement that an interrupted self-update created but never started.
//
// It also decides when a self-update may replace the running container, and
// keeps the runtime from starting an old Watchtower container again.
package selfupdate
