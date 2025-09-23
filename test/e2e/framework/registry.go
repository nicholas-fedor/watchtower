// Package framework provides registry management for e2e testing.
package framework

import (
	"context"
	"fmt"
	"log"
	"os/exec"
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
	// First tag the image for the local registry
	localImage := fmt.Sprintf("%s/%s:%s", r.url, imageName, tag)
	tagCmd := exec.Command("docker", "tag", fmt.Sprintf("%s:%s", imageName, tag), localImage)
	if output, err := tagCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to tag image for registry: %w, output: %s", err, string(output))
	}

	// Then push the image
	pushCmd := exec.Command("docker", "push", localImage)
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push image to registry: %w, output: %s", err, string(output))
	}

	log.Printf("Successfully pushed image %s to local registry", localImage)

	return nil
}

// PullImage pulls an image from the local registry.
func (r *LocalRegistry) PullImage(ctx context.Context, imageName, tag string) error {
	localImage := fmt.Sprintf("%s/%s:%s", r.url, imageName, tag)
	log.Printf("Pulling image %s from local registry", localImage)

	pullCmd := exec.Command("docker", "pull", localImage)
	if output, err := pullCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to pull image from registry: %w, output: %s", err, string(output))
	}

	log.Printf("Successfully pulled image %s from local registry", localImage)

	return nil
}

// Cleanup stops and removes the registry container.
func (r *LocalRegistry) Cleanup(ctx context.Context) error {
	timeout := 30 * time.Second

	return r.container.Stop(ctx, &timeout)
}
