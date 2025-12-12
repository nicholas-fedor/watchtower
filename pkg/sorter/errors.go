package sorter

import (
	"errors"
)

// ErrCircularReference indicates a circular dependency between containers.
var ErrCircularReference = errors.New("circular reference detected")

// CircularReferenceError represents a circular dependency error with the container name.
type CircularReferenceError struct {
	ContainerName string
}

// Error implements the error interface.
func (e CircularReferenceError) Error() string {
	return "circular reference detected: " + e.ContainerName
}

// Unwrap returns the underlying error for errors.Is compatibility.
func (e CircularReferenceError) Unwrap() error {
	return ErrCircularReference
}
