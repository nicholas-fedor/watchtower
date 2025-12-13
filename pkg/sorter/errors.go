package sorter

import (
	"errors"
	"strings"
)

// ErrCircularReference indicates a circular dependency between containers.
var ErrCircularReference = errors.New("circular reference detected")

// CircularReferenceError represents a circular dependency error with the container name and cycle path.
type CircularReferenceError struct {
	ContainerName string
	CyclePath     []string
}

// Error implements the error interface.
func (e CircularReferenceError) Error() string {
	if len(e.CyclePath) > 0 {
		var pathStrSb20 strings.Builder

		for i, name := range e.CyclePath {
			if i > 0 {
				pathStrSb20.WriteString(" -> ")
			}

			pathStrSb20.WriteString(name)
		}

		return "circular reference detected: " + pathStrSb20.String()
	}

	return "circular reference detected: " + e.ContainerName
}

// Unwrap returns the underlying error for errors.Is compatibility.
func (e CircularReferenceError) Unwrap() error {
	return ErrCircularReference
}
