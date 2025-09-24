// Package framework provides the core infrastructure for Watchtower end-to-end testing.
// It manages Docker containers, networks, and test lifecycle using testcontainers.
package framework

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	containerStopTimeout     = 30 * time.Second
	watchtowerStartupTimeout = 30 * time.Second
	helpCommandTimeout       = 10 * time.Second
	gitServerTimeout         = 60 * time.Second
	logTimeout               = 10 * time.Second
)

// E2EFramework manages the lifecycle of end-to-end tests for Watchtower.
// It provides utilities for creating isolated test environments with proper cleanup.
type E2EFramework struct {
	networkName   string
	registry      *LocalRegistry
	watchtowerImg string
	cleanupFuncs  []func() error
}

// NewE2EFramework creates a new end-to-end testing framework.
// It initializes Docker client, creates an isolated network, and sets up cleanup.
func NewE2EFramework(watchtowerImage string) (*E2EFramework, error) {
	// Create isolated network for testing
	networkName := fmt.Sprintf("watchtower-e2e-%d", time.Now().Unix())

	framework := &E2EFramework{
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
		return fmt.Errorf("cleanup errors: %w", errors.Join(errs...))
	}

	return nil
}

// CreateContainer creates a new container with the specified configuration.
// The container is automatically registered for cleanup.
func (f *E2EFramework) CreateContainer(
	req testcontainers.ContainerRequest,
) (testcontainers.Container, error) {
	req.Networks = []string{f.networkName}

	container, err := testcontainers.GenericContainer(
		context.Background(),
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	// Register container cleanup
	f.addCleanupFunc(func() error {
		timeout := containerStopTimeout

		return container.Stop(context.Background(), &timeout)
	})

	return container, nil
}

// CreateWatchtowerContainer creates a Watchtower container with the specified configuration.
func (f *E2EFramework) CreateWatchtowerContainer(args []string) (testcontainers.Container, error) {
	// Check if Git monitoring is enabled - if so, don't wait for exit since it's not implemented yet
	var waitStrategy wait.Strategy

	var exposeAPI bool

	noStartupMessage := false

	// Check if startup messages are suppressed
	for _, arg := range args {
		if arg == "--no-startup-message" {
			noStartupMessage = true

			break
		}
	}
	runOnce := false
	for _, arg := range args {
		if arg == "--run-once" {
			runOnce = true

			break
		}
	}

	// Set default wait strategy
	if runOnce {
		if noStartupMessage {
			waitStrategy = wait.ForLog("Update session completed").
				WithStartupTimeout(watchtowerStartupTimeout)
		} else {
			waitStrategy = wait.ForLog("Running a one time update").WithStartupTimeout(watchtowerStartupTimeout)
		}
	} else {
		waitStrategy = wait.ForLog("Watchtower").WithStartupTimeout(helpCommandTimeout)
	}

	for _, arg := range args {
		if arg == "--enable-git-monitoring" {
			// Git monitoring not implemented yet, so just wait for startup
			waitStrategy = wait.ForLog("Watchtower").WithStartupTimeout(helpCommandTimeout)

			break
		}

		if arg == "--help" {
			// Help command exits immediately, wait for help output
			waitStrategy = wait.ForLog("Watchtower automatically").
				WithStartupTimeout(logTimeout)

			break
		}
		// For notification flags, just wait for Watchtower to start
		if strings.HasPrefix(arg, "--notification-") {
			waitStrategy = wait.ForLog("Watchtower").WithStartupTimeout(helpCommandTimeout)

			break
		}
		// For HTTP API flags, wait for API server start
		if strings.HasPrefix(arg, "--http-api") {
			if noStartupMessage {
				waitStrategy = wait.ForLog("HTTP API is enabled").
					WithStartupTimeout(helpCommandTimeout)
			} else {
				waitStrategy = wait.ForLog("HTTP API server started successfully").
					WithStartupTimeout(helpCommandTimeout)
			}

			exposeAPI = true

			break
		}
	}

	req := testcontainers.ContainerRequest{
		Image:      f.watchtowerImg,
		Cmd:        args,
		WaitingFor: waitStrategy,
		AutoRemove: true,
		Networks:   []string{f.networkName},
		HostConfigModifier: func(hostConfig *container.HostConfig) {
			hostConfig.Binds = []string{"/var/run/docker.sock:/var/run/docker.sock"}
		},
	}

	if exposeAPI {
		req.ExposedPorts = []string{"8080/tcp"}
		originalModifier := req.HostConfigModifier
		req.HostConfigModifier = func(hostConfig *container.HostConfig) {
			originalModifier(hostConfig)
			hostConfig.PortBindings = map[nat.Port][]nat.PortBinding{
				"8080/tcp": {{HostIP: "0.0.0.0", HostPort: "8080"}},
			}
		}
	}

	container, err := testcontainers.GenericContainer(
		context.Background(),
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Watchtower container: %w", err)
	}

	// Register container cleanup
	f.addCleanupFunc(func() error {
		timeout := containerStopTimeout

		_ = container.Stop(context.Background(), &timeout)

		return nil
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
	err := wait.ForLog(logMessage).
		WithStartupTimeout(timeout).
		WaitUntilReady(context.Background(), container)
	if err != nil {
		return fmt.Errorf("failed to wait for log: %w", err)
	}

	return nil
}

// GetContainerLogs retrieves logs from a container for debugging.
func (f *E2EFramework) GetContainerLogs(container testcontainers.Container) (string, error) {
	reader, err := container.Logs(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get container logs: %w", err)
	}
	defer reader.Close()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read container logs: %w", err)
	}

	return string(logs), nil
}

// CreateLocalRegistry creates and starts a local Docker registry for testing.
func (f *E2EFramework) CreateLocalRegistry() (*LocalRegistry, error) {
	registry, err := NewLocalRegistry(context.Background())
	if err != nil {
		return nil, err
	}

	f.registry = registry
	f.addCleanupFunc(func() error {
		return registry.Cleanup(context.Background())
	})

	return registry, nil
}

// BuildAndPushImage tags an existing Docker image and pushes it to the specified registry.
func (f *E2EFramework) BuildAndPushImage(sourceImage, tag, registryURL, version string) error {
	// Tag the existing image for registry
	targetImage := fmt.Sprintf(
		"%s/%s:%s",
		registryURL,
		tag,
		version,
	) // #nosec G204 - controlled test input

	tagCmd := exec.CommandContext(context.Background(), "docker", "tag", sourceImage, targetImage)
	if output, err := tagCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to tag image: %w, output: %s", err, string(output))
	}

	// Push the image
	pushImage := fmt.Sprintf(
		"%s/%s:%s",
		registryURL,
		tag,
		version,
	) // #nosec G204 - controlled test input

	pushCmd := exec.CommandContext(context.Background(), "docker", "push", pushImage)
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push image: %w, output: %s", err, string(output))
	}

	return nil
}

// UpdateTestImage simulates updating an image by tagging an existing image with a new version.
func (f *E2EFramework) UpdateTestImage(image, oldTag, newTag string) error {
	// Tag the existing image with the new tag to simulate an update
	sourceTag := fmt.Sprintf("%s:%s", image, oldTag) // #nosec G204 - controlled test input
	targetTag := fmt.Sprintf("%s:%s", image, newTag) // #nosec G204 - controlled test input

	tagCmd := exec.CommandContext(context.Background(), "docker", "tag", sourceTag, targetTag)
	if output, err := tagCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to tag image for update: %w, output: %s", err, string(output))
	}

	// If we have a registry, push the new tag
	if f.registry != nil {
		pushTag := fmt.Sprintf("%s:%s", image, newTag) // #nosec G204 - controlled test input

		pushCmd := exec.CommandContext(context.Background(), "docker", "push", pushTag)
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
	writeScript := fmt.Sprintf(
		`echo '%s' > /etc/docker/daemon.json`,
		config,
	) // #nosec G204 - controlled test input

	writeCmd := exec.CommandContext(context.Background(), "sudo", "sh", "-c", writeScript)
	if output, err := writeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to write daemon.json: %w, output: %s", err, string(output))
	}

	// Restart Docker daemon
	restartCmd := exec.CommandContext(
		context.Background(),
		"sudo",
		"systemctl",
		"restart",
		"docker",
	)
	if output, err := restartCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restart Docker: %w, output: %s", err, string(output))
	}

	// Register cleanup to restore original daemon.json
	f.addCleanupFunc(func() error {
		// Remove the test daemon.json (system will use defaults)
		removeCmd := exec.CommandContext(
			context.Background(),
			"sudo",
			"rm",
			"-f",
			"/etc/docker/daemon.json",
		)
		if output, err := removeCmd.CombinedOutput(); err != nil {
			log.Printf(
				"Warning: failed to remove test daemon.json: %v, output: %s",
				err,
				string(output),
			)
		}

		// Restart Docker again
		restartCmd := exec.CommandContext(
			context.Background(),
			"sudo",
			"systemctl",
			"restart",
			"docker",
		)
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

// BuildWatchtowerImage builds a local Watchtower image for testing.
// This provides an alternative to the external wt.sh script with better integration.
func (f *E2EFramework) BuildWatchtowerImage(imageName, tag string) error {
	// Use docker build command for simplicity and reliability
	imageTag := fmt.Sprintf("%s:%s", imageName, tag) // #nosec G204 - controlled test input

	log.Printf(
		"Building Docker image %s using Dockerfile build/docker/Dockerfile.self-local",
		imageTag,
	)
	log.Printf("Context: current directory")

	cmd := exec.CommandContext(context.Background(), "docker", "build",
		"-f", "build/docker/Dockerfile.self-local",
		"-t", imageTag,
		".")

	log.Printf("Executing: docker build -f build/docker/Dockerfile.self-local -t %s .", imageTag)

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Build failed with output:\n%s", string(output))

		return fmt.Errorf("failed to build watchtower image: %w", err)
	}

	log.Printf("Successfully built Watchtower image: %s", imageTag)

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
