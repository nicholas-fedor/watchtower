// Package mocks provides mock implementations for testing Watchtower components.
package mocks

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"

	dockerContainer "github.com/docker/docker/api/types/container" // Alias to avoid conflict.

	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// mockIDLength defines the total length of mock container IDs.
// It ensures consistent ID formatting in test mocks.
const mockIDLength = 64

// CreateMockContainer creates a container substitute valid for testing.
// It initializes a basic container with the specified ID, name, image, and creation time.
func CreateMockContainer(id string, name string, image string, created time.Time) types.Container {
	content := dockerContainer.InspectResponse{
		ContainerJSONBase: &dockerContainer.ContainerJSONBase{
			ID:      id,
			Image:   image,
			Name:    name,
			Created: created.String(),
			HostConfig: &dockerContainer.HostConfig{
				PortBindings: map[nat.Port][]nat.PortBinding{},
			},
		},
		Config: &dockerContainer.Config{
			Image:        image,
			Labels:       make(map[string]string),
			ExposedPorts: map[nat.Port]struct{}{},
		},
	}

	return container.NewContainer(
		&content,
		CreateMockImageInfo(image),
	)
}

// CreateMockImageInfo returns a mock image info struct based on the passed image.
// It provides a minimal image representation for testing purposes.
func CreateMockImageInfo(mockImage string) *image.InspectResponse {
	return &image.InspectResponse{
		ID: mockImage,
		RepoDigests: []string{
			mockImage,
		},
	}
}

// CreateMockContainerWithImageInfo creates a container with custom image info for testing.
// It is intended for specific test cases requiring detailed image configuration.
func CreateMockContainerWithImageInfo(id string, name string, image string, created time.Time, imageInfo image.InspectResponse) types.Container {
	return CreateMockContainerWithImageInfoP(id, name, image, created, &imageInfo)
}

// CreateMockContainerWithImageInfoP creates a container with a pointer to custom image info for testing.
// It uses a pointer to the image info struct, suitable for cases where the info is pre-allocated.
func CreateMockContainerWithImageInfoP(id string, name string, image string, created time.Time, imageInfo *image.InspectResponse) types.Container {
	content := dockerContainer.InspectResponse{
		ContainerJSONBase: &dockerContainer.ContainerJSONBase{
			ID:      id,
			Image:   image,
			Name:    name,
			Created: created.String(),
		},
		Config: &dockerContainer.Config{
			Image:  image,
			Labels: make(map[string]string),
		},
	}

	return container.NewContainer(
		&content,
		imageInfo,
	)
}

// CreateMockContainerWithDigest creates a container with a specific digest for testing.
// It builds a container and overrides its image digest, useful for staleness tests.
func CreateMockContainerWithDigest(id string, name string, image string, created time.Time, digest string) types.Container {
	c := CreateMockContainer(id, name, image, created)
	c.ImageInfo().RepoDigests = []string{digest}

	return c
}

// CreateMockContainerWithConfig creates a container substitute with custom configuration for testing.
// It allows specifying running/restarting states and config details for flexible test scenarios.
func CreateMockContainerWithConfig(id string, name string, image string, running bool, restarting bool, created time.Time, config *dockerContainer.Config) types.Container {
	content := dockerContainer.InspectResponse{
		ContainerJSONBase: &dockerContainer.ContainerJSONBase{
			ID:    id,
			Image: image,
			Name:  name,
			State: &dockerContainer.State{
				Running:    running,
				Restarting: restarting,
			},
			Created: created.String(),
			HostConfig: &dockerContainer.HostConfig{
				PortBindings: map[nat.Port][]nat.PortBinding{},
			},
		},
		Config: config,
	}

	return container.NewContainer(
		&content,
		CreateMockImageInfo(image),
	)
}

// CreateContainerForProgress creates a container substitute for tracking session/update progress.
// It generates a unique ID and name based on the index and state prefix for testing progress reporting.
func CreateContainerForProgress(index int, idPrefix int, nameFormat string) (types.Container, types.ImageID) {
	indexStr := strconv.Itoa(idPrefix + index)
	// Pad the mock ID to a consistent length with zeros, accounting for the "c79" prefix.
	mockID := indexStr + strings.Repeat("0", mockIDLength-3-len(indexStr))
	contID := "c79" + mockID
	contName := fmt.Sprintf(nameFormat, index+1)
	oldImgID := "01d" + mockID
	newImgID := "d0a" + mockID
	imageName := fmt.Sprintf("mock/%s:latest", contName)
	config := &dockerContainer.Config{
		Image: imageName,
	}
	c := CreateMockContainerWithConfig(contID, contName, oldImgID, true, false, time.Now(), config)

	return c, types.ImageID(newImgID)
}

// CreateMockContainerWithLinks creates a container with specified links for testing.
// It is useful for testing dependency-related behaviors in Watchtower.
func CreateMockContainerWithLinks(id string, name string, image string, created time.Time, links []string, imageInfo *image.InspectResponse) types.Container {
	content := dockerContainer.InspectResponse{
		ContainerJSONBase: &dockerContainer.ContainerJSONBase{
			ID:      id,
			Image:   image,
			Name:    name,
			Created: created.String(),
			HostConfig: &dockerContainer.HostConfig{
				Links: links,
			},
		},
		Config: &dockerContainer.Config{
			Image:  image,
			Labels: make(map[string]string),
		},
	}

	return container.NewContainer(
		&content,
		imageInfo,
	)
}
