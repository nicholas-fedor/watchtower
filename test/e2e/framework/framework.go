// Package framework provides the core infrastructure for Watchtower end-to-end testing.
// It manages Docker containers, networks, and test lifecycle using testcontainers.
package framework

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
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
		ctx:           ctx,
		networkName:   networkName,
		watchtowerImg: watchtowerImage,
		cleanupFuncs:  []func() error{},
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
	// Check if Git monitoring is enabled - if so, don't wait for exit since it's not implemented yet
	var waitStrategy wait.Strategy = wait.ForLog("Running a one time update").WithStartupTimeout(30 * time.Second) // Wait for run-once start
	for _, arg := range args {
		if arg == "--enable-git-monitoring" {
			// Git monitoring not implemented yet, so just wait for startup
			waitStrategy = wait.ForLog("Watchtower").WithStartupTimeout(10 * time.Second)

			break
		}
		if arg == "--help" {
			// Help command exits immediately, wait for help output
			waitStrategy = wait.ForLog("Watchtower automatically").
				WithStartupTimeout(10 * time.Second)

			break
		}
		// For notification flags, just wait for Watchtower to start
		if strings.HasPrefix(arg, "--notification-") {
			waitStrategy = wait.ForLog("Watchtower").WithStartupTimeout(10 * time.Second)

			break
		}
	}

	req := testcontainers.ContainerRequest{
		Image:      f.watchtowerImg,
		Cmd:        args,
		WaitingFor: waitStrategy,
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

// CreateLocalRegistry creates and starts a local Docker registry for testing.
func (f *E2EFramework) CreateLocalRegistry() (*LocalRegistry, error) {
	registry, err := NewLocalRegistry(f.ctx)
	if err != nil {
		return nil, err
	}

	f.registry = registry
	f.addCleanupFunc(func() error {
		return registry.Cleanup(f.ctx)
	})

	return registry, nil
}

// BuildAndPushImage tags an existing Docker image and pushes it to the specified registry.
func (f *E2EFramework) BuildAndPushImage(sourceImage, tag, registryURL, version string) error {
	// Tag the existing image for registry
	tagCmd := exec.Command(
		"docker",
		"tag",
		sourceImage,
		fmt.Sprintf("%s/%s:%s", registryURL, tag, version),
	)
	if output, err := tagCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to tag image: %w, output: %s", err, string(output))
	}

	// Push the image
	pushCmd := exec.Command("docker", "push", fmt.Sprintf("%s/%s:%s", registryURL, tag, version))
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push image: %w, output: %s", err, string(output))
	}

	return nil
}

// UpdateTestImage simulates updating an image by tagging an existing image with a new version.
func (f *E2EFramework) UpdateTestImage(image, oldTag, newTag string) error {
	// Tag the existing image with the new tag to simulate an update
	tagCmd := exec.Command(
		"docker",
		"tag",
		fmt.Sprintf("%s:%s", image, oldTag),
		fmt.Sprintf("%s:%s", image, newTag),
	)
	if output, err := tagCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to tag image for update: %w, output: %s", err, string(output))
	}

	// If we have a registry, push the new tag
	if f.registry != nil {
		pushCmd := exec.Command("docker", "push", fmt.Sprintf("%s:%s", image, newTag))
		if output, err := pushCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to push updated image: %w, output: %s", err, string(output))
		}
	}

	return nil
}

// ConfigureInsecureRegistry configures Docker to allow insecure access to a registry.
// This is commonly needed for local registries in testing.
func (f *E2EFramework) ConfigureInsecureRegistry(registryURL string) error {
	// Create daemon.json with insecure registry configuration
	config := fmt.Sprintf(`{"insecure-registries": ["%s"]}`, registryURL)

	// Write to /etc/docker/daemon.json (requires sudo)
	writeCmd := exec.Command(
		"sudo",
		"sh",
		"-c",
		fmt.Sprintf(`echo '%s' > /etc/docker/daemon.json`, config),
	)
	if output, err := writeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to write daemon.json: %w, output: %s", err, string(output))
	}

	// Restart Docker daemon
	restartCmd := exec.Command("sudo", "systemctl", "restart", "docker")
	if output, err := restartCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restart Docker: %w, output: %s", err, string(output))
	}

	// Register cleanup to restore original daemon.json
	f.addCleanupFunc(func() error {
		// Remove the test daemon.json (system will use defaults)
		removeCmd := exec.Command("sudo", "rm", "-f", "/etc/docker/daemon.json")
		if output, err := removeCmd.CombinedOutput(); err != nil {
			log.Printf(
				"Warning: failed to remove test daemon.json: %v, output: %s",
				err,
				string(output),
			)
		}

		// Restart Docker again
		restartCmd := exec.Command("sudo", "systemctl", "restart", "docker")
		if output, err := restartCmd.CombinedOutput(); err != nil {
			log.Printf(
				"Warning: failed to restart Docker after cleanup: %v, output: %s",
				err,
				string(output),
			)
		}

		return nil
	})

	return nil
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
