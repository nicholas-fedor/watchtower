package sorter

import (
	"errors"
	"strings"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// ErrCircularReference indicates a circular dependency between containers.
var ErrCircularReference = errors.New("circular reference detected")

// ErrIdentifierCollision indicates an identifier collision between containers.
var ErrIdentifierCollision = errors.New("identifier collision detected")

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

// IdentifierCollisionError represents an error when multiple containers have the same normalized identifier.
type IdentifierCollisionError struct {
	DuplicateIdentifier string
	AffectedContainers  []types.Container
}

// Error implements the error interface.
func (e IdentifierCollisionError) Error() string {
	containerDetails := make([]string, 0, len(e.AffectedContainers))
	for _, c := range e.AffectedContainers {
		containerDetails = append(containerDetails, c.Name()+" ("+c.ID().ShortID()+")")
	}

	return "identifier collision detected: '" + e.DuplicateIdentifier + "' used by containers: " + strings.Join(
		containerDetails,
		", ",
	)
}

// Unwrap returns the underlying error for errors.Is compatibility.
func (e IdentifierCollisionError) Unwrap() error {
	return ErrIdentifierCollision
}
