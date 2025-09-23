# End-to-End Testing Framework

This directory contains the end-to-end (e2e) testing framework for Watchtower, following the [Go project layout standards](https://github.com/golang-standards/project-layout/tree/master/test).

## Overview

The e2e testing framework is built on [Testcontainers for Go](https://golang.testcontainers.org/), providing:

- **Isolated test environments** using Docker containers and networks
- **Automatic resource cleanup** to prevent test interference
- **Reusable test utilities** for common Watchtower testing scenarios
- **Multi-environment support** for testing different Docker versions and registries
- **Programmatic container lifecycle management** for complex test scenarios

## Directory Structure

```
test/
├── README.md                    # This file
└── e2e/                        # End-to-end tests
    ├── framework/              # Core testing framework
    │   ├── framework.go        # Main framework implementation
    │   └── registry.go         # Local registry management
    ├── scenarios/              # Test scenarios by category
    │   ├── lifecycle/          # Lifecycle hook tests
    │   ├── networking/         # Container networking tests
    │   ├── git/                # Git monitoring tests
    │   ├── registry/           # Registry integration tests
    │   └── docker/             # Docker version compatibility
    ├── fixtures/               # Test data and containers
    │   ├── images/             # Pre-built test images
    │   ├── configs/            # Configuration files
    │   └── scripts/            # Helper utilities
    └── suites/                 # Test suite definitions
        ├── basic_test.go       # Basic functionality tests
        ├── advanced_test.go    # Advanced feature tests
        └── regression_test.go  # Regression tests
```

## Quick Start

### Running E2E Tests

```bash
# Run all e2e tests
go test ./test/e2e/...

# Run specific test suite
go test ./test/e2e/suites/ -run TestBasicSuite

# Run with verbose output
go test -v ./test/e2e/suites/
```

### Basic Test Example

```go
package suites

import (
    "testing"
    "github.com/nicholas-fedor/watchtower/test/e2e/framework"
)

func TestBasicWatchtower(t *testing.T) {
    // Create test framework
    fw, err := framework.NewE2EFramework("watchtower:test")
    if err != nil {
        t.Fatalf("Failed to create framework: %v", err)
    }

    // Run test with automatic cleanup
    fw.RunTestWithCleanup(t, func() error {
        // Create a test container
        container, err := fw.CreateContainer(testcontainers.ContainerRequest{
            Image: "nginx:alpine",
            ExposedPorts: []string{"80/tcp"},
        })
        if err != nil {
            return fmt.Errorf("failed to create container: %w", err)
        }

        // Run Watchtower
        watchtower, err := fw.CreateWatchtowerContainer([]string{"--run-once"})
        if err != nil {
            return fmt.Errorf("failed to create watchtower: %w", err)
        }

        // Wait for Watchtower to complete
        return fw.WaitForLog(watchtower, "Session finished", 30*time.Second)
    })
}
```

## Framework Components

### E2EFramework

The main testing framework that manages:

- **Container lifecycle**: Automatic creation, cleanup, and networking
- **Resource isolation**: Each test runs in its own Docker network
- **Error handling**: Comprehensive error reporting and debugging
- **Test utilities**: Common operations like waiting for logs, checking health

### Local Registry Management

Handles Docker registry operations for testing image push/pull scenarios:

- **Insecure registry setup**: Automatic daemon.json configuration
- **Registry lifecycle**: Creation, configuration, and cleanup
- **Image management**: Build, tag, push, and pull operations

### Test Scenarios

Organized by functional area:

- **Basic**: Core Watchtower functionality validation
- **Git**: Git repository monitoring and updates
- **Registry**: Docker registry integrations (Docker Hub, GHCR, Harbor)
- **Networking**: Container networking and dependencies
- **Lifecycle**: Pre/post-update hooks and custom commands
- **API**: HTTP API endpoints and authentication
- **Notifications**: All supported notification systems
- **Scheduling**: Cron expressions and timing operations
- **Advanced**: Rolling restarts, scope isolation, label precedence

### Local Registry

Manages a local Docker registry for testing image operations without external dependencies.

### Test Scenarios

Pre-built test scenarios for common Watchtower use cases:

- **Lifecycle hooks**: Testing pre/post-update commands
- **Container networking**: Testing with linked containers and custom networks
- **Git monitoring**: Testing Git-based container updates
- **Registry integration**: Testing with different container registries
- **Docker compatibility**: Testing across Docker versions

## Environment Setup

### Prerequisites

- Docker 20.10+ with Docker-in-Docker support
- Go 1.19+ for testcontainers
- Sufficient disk space for test containers

### Configuration

Tests can be configured via environment variables:

```bash
# Watchtower image to test
export WATCHTOWER_IMAGE="watchtower:test"

# Docker registry credentials (for registry tests)
export DOCKER_USERNAME="..."
export DOCKER_PASSWORD="..."

# Git credentials (for Git monitoring tests)
export GITHUB_TOKEN="..."
```

## Implementation Details

### Core Architecture

The framework uses Testcontainers for Go to provide:

- **Docker API Integration**: Direct interaction with Docker daemon
- **Network Isolation**: Automatic network creation per test
- **Resource Management**: Guaranteed cleanup via defer statements
- **Container Orchestration**: Multi-container test scenarios

### Key Classes

- **E2EFramework**: Main test orchestration class
- **LocalRegistry**: Manages test Docker registries
- **Container Operations**: Wrappers for common Docker operations

### Test Lifecycle

1. **Setup**: Framework initialization and network creation
2. **Test Execution**: Container creation, Watchtower execution, validation
3. **Cleanup**: Automatic resource removal and network teardown

## Writing Tests

### Test Structure

```go
func TestMyFeature(t *testing.T) {
 fw, err := framework.NewE2EFramework("watchtower:test")
 require.NoError(t, err)

 fw.RunTestWithCleanup(t, func() error {
  // Test implementation
  return nil
 })
}
```

### Advanced Test Patterns

#### Testing Container Updates

```go
// Create test container with updateable image
container, err := fw.CreateContainer(testcontainers.ContainerRequest{
 Image: "nginx:1.20",
 Labels: map[string]string{
  "com.centurylinklabs.watchtower.enable": "true",
 },
})

// Simulate image update (registry operations)
err = fw.UpdateTestImage("nginx", "1.20", "1.21")

// Run Watchtower
watchtower, err := fw.CreateWatchtowerContainer([]string{"--run-once"})

// Verify update occurred
err = fw.WaitForLog(watchtower, "Found new .* image", 60*time.Second)
```

#### Testing Git Monitoring

```go
// Setup mock Git repository
repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "abc123")

// Create container with Git labels
container, err := fw.CreateContainer(testcontainers.ContainerRequest{
 Image: "nginx:alpine",
 Labels: map[string]string{
  "com.centurylinklabs.watchtower.git-repo": repoURL,
  "com.centurylinklabs.watchtower.git-branch": "main",
 },
})

// Simulate Git commit
err = fw.SimulateGitCommit(repoURL, "def456")

// Run Watchtower and verify Git monitoring
watchtower, err := fw.CreateWatchtowerContainer([]string{"--run-once"})
err = fw.WaitForLog(watchtower, "Found new .* commit", 60*time.Second)
```

### Best Practices

1. **Use RunTestWithCleanup**: Ensures proper resource cleanup
2. **Check errors**: Always handle and return errors appropriately
3. **Use timeouts**: Set reasonable timeouts for async operations
4. **Log debugging info**: Use fw.GetContainerLogs() for troubleshooting
5. **Test isolation**: Each test should be independent
6. **Resource naming**: Use unique names to avoid conflicts
7. **Async operations**: Use WaitForLog for reliable timing
8. **Error context**: Provide detailed error messages for debugging

### Common Patterns

#### Testing Container Updates

```go
// Create initial container
container, err := fw.CreateContainer(testcontainers.ContainerRequest{
    Image: "nginx:1.20",
    Labels: map[string]string{
        "com.centurylinklabs.watchtower.enable": "true",
    },
})

// Update to new version
// (Implementation depends on registry setup)

// Run Watchtower
watchtower, err := fw.CreateWatchtowerContainer([]string{"--run-once"})

// Verify update occurred
return fw.WaitForLog(watchtower, "Found new .* image", 60*time.Second)
```

#### Testing Lifecycle Hooks

```go
container, err := fw.CreateContainer(testcontainers.ContainerRequest{
    Image: "test-app:v1",
    Labels: map[string]string{
        "com.centurylinklabs.watchtower.lifecycle.pre-update": "echo 'pre-update'",
        "com.centurylinklabs.watchtower.lifecycle.post-update": "echo 'post-update'",
    },
})

// Run update and verify hooks executed
// Check container logs for hook output
```

## Debugging

### Getting Container Logs

```go
logs, err := fw.GetContainerLogs(container)
if err != nil {
    t.Logf("Container logs: %s", logs)
}
```

### Manual Test Inspection

```bash
# List running containers
docker ps

# Check container logs
docker logs <container-id>

# Inspect networks
docker network ls
docker network inspect <network-name>
```

## Contributing

When adding new tests:

1. **Follow naming conventions**: `TestXxx` for test functions
2. **Add to appropriate suite**: Place in relevant scenario directory
3. **Document test purpose**: Clear test names and comments
4. **Handle cleanup**: Use framework cleanup or manual cleanup
5. **Test edge cases**: Include error conditions and edge cases

## Implementation Guide

For detailed implementation instructions, patterns, and examples, see [`E2E_IMPLEMENTATION_GUIDE.md`](E2E_IMPLEMENTATION_GUIDE.md).

## Migration from Bash Scripts

Existing bash scripts in the root directory should be gradually migrated to this framework:

- `scripts/lifecycle-tests.sh` → `test/e2e/scenarios/lifecycle/`
- `scripts/contnet-tests.sh` → `test/e2e/scenarios/networking/`
- Custom local scripts → Appropriate scenario directories

This provides better maintainability, reliability, and integration with the Go testing ecosystem.

## Migration from Bash Scripts

Existing bash scripts in the root directory should be gradually migrated to this framework:

- `scripts/lifecycle-tests.sh` → `test/e2e/scenarios/lifecycle/`
- `scripts/contnet-tests.sh` → `test/e2e/scenarios/networking/`
- Custom local scripts → Appropriate scenario directories

This provides better maintainability, reliability, and integration with the Go testing ecosystem.
