// Package framework provides registry management for e2e testing.
package framework

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// LocalRegistry manages a local Docker registry for testing.
// It provides isolation for testing image operations without affecting external registries.
type LocalRegistry struct {
	container testcontainers.Container
	url       string
}

// NewLocalRegistry creates and starts a local Docker registry container.
// The registry is configured for testing with automatic cleanup.
func NewLocalRegistry(ctx context.Context) (*LocalRegistry, error) {
	req := testcontainers.ContainerRequest{
		Image:        "registry:2",
		ExposedPorts: []string{"5000/tcp"},
		WaitingFor:   wait.ForListeningPort("5000/tcp").WithStartupTimeout(60 * time.Second),
		AutoRemove:   true,
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start registry container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx) // cleanup on error

		return nil, fmt.Errorf("failed to get registry host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5000")
	if err != nil {
		container.Terminate(ctx) // cleanup on error

		return nil, fmt.Errorf("failed to get registry port: %w", err)
	}

	url := fmt.Sprintf("%s:%s", host, port.Port())

	registry := &LocalRegistry{
		container: container,
		url:       url,
	}

	log.Printf("Local registry started at: %s", url)

	return registry, nil
}

// URL returns the registry URL for pushing/pulling images.
func (r *LocalRegistry) URL() string {
	return r.url
}

// PushImage pushes an image to the local registry.
// This is useful for testing image operations in isolation.
func (r *LocalRegistry) PushImage(ctx context.Context, imageName, tag string) error {
	// Tag the image for the local registry
	localImage := fmt.Sprintf("%s/%s:%s", r.url, imageName, tag)

	// Use docker CLI to tag and push (could be improved with Docker client)
	log.Printf("Pushing image %s to local registry", localImage)

	return fmt.Errorf("PushImage not implemented - requires Docker client integration")
}

// PullImage pulls an image from the local registry.
func (r *LocalRegistry) PullImage(ctx context.Context, imageName, tag string) error {
	localImage := fmt.Sprintf("%s/%s:%s", r.url, imageName, tag)
	log.Printf("Pulling image %s from local registry", localImage)

	return fmt.Errorf("PullImage not implemented - requires Docker client integration")
}

// Cleanup stops and removes the registry container.
func (r *LocalRegistry) Cleanup(ctx context.Context) error {
	timeout := 30 * time.Second

	return r.container.Stop(ctx, &timeout)
}
