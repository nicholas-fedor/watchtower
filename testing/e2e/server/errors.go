package server

import "errors"

var (
	// ErrNotReady means Listen did not become healthy in time.
	ErrNotReady = errors.New("control plane not ready")
)
