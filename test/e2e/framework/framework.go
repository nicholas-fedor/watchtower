// Package framework provides the core infrastructure for Watchtower end-to-end testing.
// It manages Docker containers, networks, and test lifecycle using testcontainers.
package framework

import (
	"context"
	"fmt"
	"io"
	"log"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// E2EFramework manages the lifecycle of end-to-end tests for Watchtower.
// It provides utilities for creating isolated test environments with proper cleanup.
type E2EFramework struct {
	ctx           context.Context
	networkName   string
	registry      *LocalRegistry
	watchtowerImg string
	cleanupFuncs  []func() error
}

// NewE2EFramework creates a new end-to-end testing framework.
// It initializes Docker client, creates an isolated network, and sets up cleanup.
func NewE2EFramework(watchtowerImage string) (*E2EFramework, error) {
	ctx := context.Background()

	// Create isolated network for testing
	networkName := fmt.Sprintf("watchtower-e2e-%d", time.Now().Unix())

	framework := &E2EFramework{
		ctx:          ctx,
		networkName:  networkName,
		cleanupFuncs: []func() error{},
	}

	return framework, nil
}

// addCleanupFunc registers a cleanup function to be called during teardown.
func (f *E2EFramework) addCleanupFunc(cleanup func() error) {
	f.cleanupFuncs = append(f.cleanupFuncs, cleanup)
}

// Cleanup performs teardown of all test resources.
// It should be called in test cleanup functions or defer statements.
func (f *E2EFramework) Cleanup() error {
	var errs []error
	for _, cleanup := range f.cleanupFuncs {
		if err := cleanup(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}

	return nil
}

// CreateContainer creates a new container with the specified configuration.
// The container is automatically registered for cleanup.
func (f *E2EFramework) CreateContainer(
	req testcontainers.ContainerRequest,
) (testcontainers.Container, error) {
	req.Networks = []string{f.networkName}

	container, err := testcontainers.GenericContainer(f.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Register container cleanup
	f.addCleanupFunc(func() error {
		timeout := 30 * time.Second

		return container.Stop(f.ctx, &timeout)
	})

	return container, nil
}

// CreateWatchtowerContainer creates a Watchtower container with the specified configuration.
func (f *E2EFramework) CreateWatchtowerContainer(args []string) (testcontainers.Container, error) {
	req := testcontainers.ContainerRequest{
		Image: f.watchtowerImg,
		Cmd:   args,
		WaitingFor: wait.ForLog("Watchtower is waiting for changes").
			WithStartupTimeout(60 * time.Second),
		AutoRemove: true,
		Networks:   []string{f.networkName},
		Mounts: testcontainers.ContainerMounts{
			{
				Source: testcontainers.DockerBindMountSource{
					HostPath: "/var/run/docker.sock",
				},
				Target: "/var/run/docker.sock",
			},
		},
	}

	container, err := testcontainers.GenericContainer(f.ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Watchtower container: %w", err)
	}

	// Register container cleanup
	f.addCleanupFunc(func() error {
		timeout := 30 * time.Second

		return container.Stop(f.ctx, &timeout)
	})

	return container, nil
}

// RunTestWithCleanup runs a test function with automatic cleanup.
// This is a convenience method that ensures cleanup happens even if the test fails.
func (f *E2EFramework) RunTestWithCleanup(t *testing.T, testFunc func() error) {
	t.Helper()

	defer func() {
		if err := f.Cleanup(); err != nil {
			t.Logf("Cleanup failed: %v", err)
		}
	}()

	if err := testFunc(); err != nil {
		t.Fatalf("Test failed: %v", err)
	}
}

// WaitForLog waits for a specific log message in a container.
func (f *E2EFramework) WaitForLog(
	container testcontainers.Container,
	logMessage string,
	timeout time.Duration,
) error {
	return wait.ForLog(logMessage).WithStartupTimeout(timeout).WaitUntilReady(f.ctx, container)
}

// GetContainerLogs retrieves logs from a container for debugging.
func (f *E2EFramework) GetContainerLogs(container testcontainers.Container) (string, error) {
	reader, err := container.Logs(f.ctx)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}

// LogTestInfo logs useful information about the current test environment.
func (f *E2EFramework) LogTestInfo() {
	log.Printf("E2E Framework initialized:")
	log.Printf("  - Network: %s", f.networkName)
	log.Printf("  - Watchtower Image: %s", f.watchtowerImg)
	if f.registry != nil {
		log.Printf("  - Registry: %s", f.registry.URL())
	}
}
