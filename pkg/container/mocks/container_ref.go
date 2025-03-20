package mocks

import (
	"fmt"
	"os"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// imageRef represents a mock image reference with an ID and file path.
type imageRef struct {
	id   types.ImageID
	file string
}

// getFileName returns the full file path for the mock image JSON file.
func (ir *imageRef) getFileName() string {
	return fmt.Sprintf("./mocks/data/image_%v.json", ir.file)
}

// ContainerRef represents a mock container with associated metadata.
// Includes name, ID, image reference, file path, linked containers, and existence status.
type ContainerRef struct {
	name       string
	id         types.ContainerID
	image      *imageRef
	file       string
	references []*ContainerRef
	isMissing  bool
}

// Uses the explicit file if set, otherwise falls back to the container name; returns an error if the file doesn’t exist.
func (cr *ContainerRef) getContainerFile() (string, error) {
	file := cr.file
	if file == "" {
		file = cr.name
	}

	containerFile := fmt.Sprintf("./mocks/data/container_%v.json", file)
	_, err := os.Stat(containerFile)
	if err != nil {
		return containerFile, fmt.Errorf("failed to stat mock container file %s: %w", containerFile, err)
	}

	return containerFile, nil
}

// ContainerID returns the mock container’s ID.
func (cr *ContainerRef) ContainerID() types.ContainerID {
	return cr.id
}
