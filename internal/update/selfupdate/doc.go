// Package selfupdate replaces Watchtower's own container through an ephemeral
// orchestrator.
//
// The running Watchtower starts a short-lived orchestrator container from the
// new image. The orchestrator stops the old container and starts its
// replacement, then removes the old container. The package also starts a
// replacement that an interrupted self-update created but never started.
//
// It also decides when a self-update may replace the running container, and
// keeps the runtime from starting an old Watchtower container again.
package selfupdate
