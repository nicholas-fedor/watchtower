package store

import "errors"

var (
	// ErrNotFound means the run or case does not exist.
	ErrNotFound = errors.New("store: not found")
	// ErrConflict means the status transition is illegal.
	ErrConflict = errors.New("store: conflict")
	// ErrNotConfigured means a Postgres DSN was required but empty.
	ErrNotConfigured = errors.New("store: database url not configured")
)
