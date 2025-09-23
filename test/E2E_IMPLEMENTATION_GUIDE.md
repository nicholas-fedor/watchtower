# Watchtower E2E Testing Implementation Guide

## Overview

This document provides a comprehensive guide for implementing and extending the Watchtower end-to-end testing framework. The framework is built on [Testcontainers for Go](https://golang.testcontainers.org/) and provides isolated, automated testing of Watchtower's container monitoring and update functionality.

## Architecture

### Core Components

```
test/e2e/
├── framework/              # Core testing infrastructure
│   ├── framework.go        # Main E2EFramework class
│   └── registry.go         # Local registry management
├── scenarios/              # Test scenarios by category
│   ├── basic/             # Core functionality tests
│   ├── git/               # Git monitoring tests
│   ├── registry/          # Registry integration tests
│   ├── networking/        # Container networking tests
│   ├── lifecycle/         # Lifecycle hook tests
│   ├── api/               # HTTP API tests
│   ├── notifications/     # Notification system tests
│   ├── scheduling/        # Cron and timing tests
│   └── advanced/          # Advanced feature tests
├── fixtures/               # Test data and containers
│   ├── images/            # Pre-built test images
│   ├── configs/           # Configuration files
│   └── scripts/           # Helper utilities
└── suites/                 # Test suite definitions
    ├── basic_test.go      # Basic functionality
    ├── advanced_test.go   # Advanced features
    └── regression_test.go # Regression tests
```

### Framework Classes

#### E2EFramework

Main orchestration class providing:

- **Container Management**: Create, start, stop containers
- **Network Isolation**: Automatic Docker network creation
- **Resource Cleanup**: Guaranteed teardown via defer
- **Watchtower Integration**: Specialized Watchtower container creation
- **Logging Utilities**: Container log retrieval and analysis

#### LocalRegistry

Manages test Docker registries:

- **Insecure Registry Setup**: Automatic daemon.json configuration
- **Registry Lifecycle**: Create, configure, cleanup
- **Image Operations**: Build, tag, push, pull
- **TLS Configuration**: Certificate and security management

## Implementation Phases

### Phase 1: Framework Validation

#### 1.1 Environment Setup

```bash
# Verify Go and Docker versions
go version  # Should be 1.25.1+
docker --version  # Should be 20.10+

# Run existing basic tests
go test ./test/e2e/suites/ -v
```

#### 1.2 Framework Enhancement

- Add registry management helpers
- Implement Git repository mocking
- Create notification testing utilities
- Add HTTP API client helpers

### Phase 2: Core Functionality Tests

#### 2.1 Basic Container Operations

```go
func TestBasicContainerUpdate(t *testing.T) {
    fw, err := framework.NewE2EFramework("watchtower:test")
    require.NoError(t, err)

    fw.RunTestWithCleanup(t, func() error {
        // Create test container
        container, err := fw.CreateContainer(testcontainers.ContainerRequest{
            Image: "nginx:1.20",
            Labels: map[string]string{
                "com.centurylinklabs.watchtower.enable": "true",
            },
        })
        require.NoError(t, err)

        // Simulate image update
        err = fw.UpdateTestImage("nginx", "1.20", "1.21")
        require.NoError(t, err)

        // Run Watchtower
        watchtower, err := fw.CreateWatchtowerContainer([]string{"--run-once"})
        require.NoError(t, err)

        // Verify update
        return fw.WaitForLog(watchtower, "Found new .* image", 60*time.Second)
    })
}
```

#### 2.2 Registry Integration Tests

```go
func TestLocalRegistryIntegration(t *testing.T) {
    fw, err := framework.NewE2EFramework("watchtower:test")
    require.NoError(t, err)

    fw.RunTestWithCleanup(t, func() error {
        // Setup local registry
        registry, err := fw.CreateLocalRegistry()
        require.NoError(t, err)

        // Build and push test image
        imageName := fmt.Sprintf("%s/test-app", registry.URL())
        err = fw.BuildAndPushImage("test-app", "v1", imageName, "v1")
        require.NoError(t, err)

        // Create container from registry
        container, err := fw.CreateContainer(testcontainers.ContainerRequest{
            Image: imageName + ":v1",
            Labels: map[string]string{
                "com.centurylinklabs.watchtower.enable": "true",
            },
        })
        require.NoError(t, err)

        // Update image in registry
        err = fw.BuildAndPushImage("test-app", "v2", imageName, "v2")
        require.NoError(t, err)

        // Run Watchtower and verify update
        watchtower, err := fw.CreateWatchtowerContainer([]string{"--run-once"})
        require.NoError(t, err)

        return fw.WaitForLog(watchtower, "Found new .* image", 60*time.Second)
    })
}
```

### Phase 3: Git Monitoring Tests

#### 3.1 Git Repository Setup

```go
func TestGitMonitoringBasic(t *testing.T) {
    fw, err := framework.NewE2EFramework("watchtower:test")
    require.NoError(t, err)

    fw.RunTestWithCleanup(t, func() error {
        // Setup mock Git repository
        repoURL, cleanup := fw.SetupMockGitRepo("test-repo", "main", "abc123")
        defer cleanup()

        // Create container with Git labels
        container, err := fw.CreateContainer(testcontainers.ContainerRequest{
            Image: "nginx:alpine",
            Labels: map[string]string{
                "com.centurylinklabs.watchtower.git-repo": repoURL,
                "com.centurylinklabs.watchtower.git-branch": "main",
                "com.centurylinklabs.watchtower.enable": "true",
            },
        })
        require.NoError(t, err)

        // Simulate Git commit
        err = fw.SimulateGitCommit(repoURL, "def456")
        require.NoError(t, err)

        // Run Watchtower with Git monitoring enabled
        watchtower, err := fw.CreateWatchtowerContainer([]string{
            "--run-once",
            "--enable-git-monitoring",
        })
        require.NoError(t, err)

        // Verify Git monitoring worked
        return fw.WaitForLog(watchtower, "Found new .* commit", 60*time.Second)
    })
}
```

#### 3.2 Authentication Testing

```go
func TestGitAuthentication(t *testing.T) {
    tests := []struct {
        name     string
        authType string
        setup    func(*framework.E2EFramework) (string, func())
    }{
        {"Token Auth", "token", setupTokenAuth},
        {"SSH Auth", "ssh", setupSSHAuth},
        {"OAuth Auth", "oauth", setupOAuthAuth},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            fw, err := framework.NewE2EFramework("watchtower:test")
            require.NoError(t, err)

            fw.RunTestWithCleanup(t, func() error {
                repoURL, cleanup := tt.setup(fw)
                defer cleanup()

                // Test Git monitoring with authentication
                container, err := fw.CreateContainer(testcontainers.ContainerRequest{
                    Image: "nginx:alpine",
                    Labels: map[string]string{
                        "com.centurylinklabs.watchtower.git-repo": repoURL,
                        "com.centurylinklabs.watchtower.git-branch": "main",
                    },
                })
                require.NoError(t, err)

                watchtower, err := fw.CreateWatchtowerContainer([]string{
                    "--run-once",
                    "--enable-git-monitoring",
                    "--git-auth-token=test-token",
                })
                require.NoError(t, err)

                return fw.WaitForLog(watchtower, "Successfully authenticated", 30*time.Second)
            })
        })
    }
}
```

### Phase 4: Advanced Feature Tests

#### 4.1 Lifecycle Hooks

```go
func TestLifecycleHooks(t *testing.T) {
    fw, err := framework.NewE2EFramework("watchtower:test")
    require.NoError(t, err)

    fw.RunTestWithCleanup(t, func() error {
        // Create container with lifecycle hooks
        container, err := fw.CreateContainer(testcontainers.ContainerRequest{
            Image: "nginx:1.20",
            Labels: map[string]string{
                "com.centurylinklabs.watchtower.enable": "true",
                "com.centurylinklabs.watchtower.lifecycle.pre-update": "echo 'pre-update'",
                "com.centurylinklabs.watchtower.lifecycle.post-update": "echo 'post-update'",
            },
        })
        require.NoError(t, err)

        // Update image
        err = fw.UpdateTestImage("nginx", "1.20", "1.21")
        require.NoError(t, err)

        // Run Watchtower with lifecycle hooks enabled
        watchtower, err := fw.CreateWatchtowerContainer([]string{
            "--run-once",
            "--enable-lifecycle-hooks",
        })
        require.NoError(t, err)

        // Verify hooks executed
        err = fw.WaitForLog(watchtower, "pre-update", 30*time.Second)
        require.NoError(t, err)

        err = fw.WaitForLog(watchtower, "post-update", 30*time.Second)
        require.NoError(t, err)

        return nil
    })
}
```

#### 4.2 HTTP API Testing

```go
func TestHTTPAPI(t *testing.T) {
    fw, err := framework.NewE2EFramework("watchtower:test")
    require.NoError(t, err)

    fw.RunTestWithCleanup(t, func() error {
        // Create test container
        container, err := fw.CreateContainer(testcontainers.ContainerRequest{
            Image: "nginx:1.20",
            Labels: map[string]string{
                "com.centurylinklabs.watchtower.enable": "true",
            },
        })
        require.NoError(t, err)

        // Start Watchtower with HTTP API
        watchtower, err := fw.CreateWatchtowerContainer([]string{
            "--http-api-update",
            "--http-api-token=test-token",
        })
        require.NoError(t, err)

        // Wait for API to be ready
        err = fw.WaitForLog(watchtower, "HTTP API is enabled", 30*time.Second)
        require.NoError(t, err)

        // Trigger update via API
        err = fw.TriggerAPIUpdate("test-token", []string{"nginx"})
        require.NoError(t, err)

        // Verify update occurred
        return fw.WaitForLog(watchtower, "Found new .* image", 60*time.Second)
    })
}
```

#### 4.3 Notification Testing

```go
func TestNotifications(t *testing.T) {
    notificationTests := []struct {
        name         string
        notifierType string
        config       map[string]string
    }{
        {"Slack", "slack", map[string]string{"SLACK_HOOK_URL": "http://mock-slack:8080"}},
        {"Email", "email", map[string]string{
            "EMAIL_FROM": "test@example.com",
            "EMAIL_TO": "admin@example.com",
        }},
        {"Gotify", "gotify", map[string]string{"GOTIFY_URL": "http://mock-gotify:8080"}},
    }

    for _, tt := range notificationTests {
        t.Run(tt.name, func(t *testing.T) {
            fw, err := framework.NewE2EFramework("watchtower:test")
            require.NoError(t, err)

            fw.RunTestWithCleanup(t, func() error {
                // Setup mock notification service
                mockService, err := fw.StartMockNotificationService(tt.notifierType)
                require.NoError(t, err)

                // Create container and update
                container, err := fw.CreateContainer(testcontainers.ContainerRequest{
                    Image: "nginx:1.20",
                    Labels: map[string]string{
                        "com.centurylinklabs.watchtower.enable": "true",
                    },
                })
                require.NoError(t, err)

                err = fw.UpdateTestImage("nginx", "1.20", "1.21")
                require.NoError(t, err)

                // Run Watchtower with notifications
                watchtower, err := fw.CreateWatchtowerContainer(fw.BuildNotificationArgs(tt.notifierType, tt.config))
                require.NoError(t, err)

                // Verify notification sent
                return fw.WaitForNotification(mockService, "Container updated", 30*time.Second)
            })
        })
    }
}
```

### Phase 5: Framework Utilities Implementation

#### 5.1 Registry Management Helpers

```go
// framework/registry.go additions
func (f *E2EFramework) CreateLocalRegistry() (*LocalRegistry, error) {
    // Implementation for insecure registry setup
}

func (f *E2EFramework) BuildAndPushImage(dockerfile, tag, registryURL, version string) error {
    // Implementation for image build and push
}

func (f *E2EFramework) UpdateTestImage(image, oldTag, newTag string) error {
    // Implementation for simulating image updates
}
```

#### 5.2 Git Testing Helpers

```go
func (f *E2EFramework) SetupMockGitRepo(name, branch, commit string) (string, func()) {
    // Create mock Git repository for testing
}

func (f *E2EFramework) SimulateGitCommit(repoURL, newCommit string) error {
    // Simulate new commit in test repository
}
```

#### 5.3 API Testing Helpers

```go
func (f *E2EFramework) TriggerAPIUpdate(token string, images []string) error {
    // Send HTTP request to Watchtower API
}

func (f *E2EFramework) GetAPIMetrics(token string) (map[string]interface{}, error) {
    // Retrieve metrics from API endpoint
}
```

## Best Practices

### Test Organization

1. **Categorize by Feature**: Group tests by functionality (Git, API, notifications)
2. **Independent Tests**: Each test should be self-contained
3. **Clear Naming**: Use descriptive test names indicating what is tested
4. **Resource Cleanup**: Always use RunTestWithCleanup for automatic cleanup

### Error Handling

1. **Detailed Errors**: Provide context in error messages
2. **Timeout Management**: Use appropriate timeouts for async operations
3. **Log Analysis**: Use WaitForLog for reliable verification
4. **Retry Logic**: Implement retries for flaky operations

### Performance Considerations

1. **Parallel Execution**: Design tests to run in parallel when possible
2. **Resource Limits**: Set appropriate CPU/memory limits for containers
3. **Cleanup Verification**: Ensure resources are properly cleaned up
4. **Network Efficiency**: Minimize network operations in tests

## Migration from Bash Scripts

### Strategy

1. **Identify Core Logic**: Extract test logic from bash scripts
2. **Create Go Equivalents**: Implement functionality using Testcontainers
3. **Preserve Test Coverage**: Ensure all scenarios are covered
4. **Gradual Migration**: Migrate scripts incrementally

### Example Migration

```bash
# Old bash script
docker run -d --name registry -p 5000:5000 registry:2
docker build -t localhost:5000/test .
docker push localhost:5000/test
```

```go
// New Go implementation
registry, err := fw.CreateLocalRegistry()
err = fw.BuildAndPushImage("test", "latest", registry.URL()+"/test", "latest")
```

## Continuous Integration

### GitHub Actions Setup

```yaml
name: E2E Tests
on: [push, pull_request]

jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.25.1'
      - name: Run E2E Tests
        run: go test ./test/e2e/... -v
```

### Docker Requirements

- Docker 20.10+ with Docker-in-Docker support
- Sufficient disk space for test containers
- Network access for external registries (if needed)

## Troubleshooting

### Common Issues

1. **Container Startup Failures**: Check Docker daemon status
2. **Network Isolation**: Verify network creation and cleanup
3. **Registry Access**: Ensure insecure registry configuration
4. **Timeout Issues**: Adjust timeouts for slower environments

### Debugging Techniques

1. **Container Logs**: Use GetContainerLogs() for debugging
2. **Network Inspection**: Check Docker network status
3. **Registry Verification**: Test registry accessibility manually
4. **Verbose Logging**: Enable debug logging in Watchtower

## Future Enhancements

### Planned Features

1. **Multi-Architecture Testing**: Support for ARM64/AMD64 testing
2. **Kubernetes Integration**: Test with Kubernetes environments
3. **Performance Testing**: Load testing and performance benchmarks
4. **Chaos Engineering**: Network failures and container crashes

### Framework Improvements

1. **Test Parallelization**: Better support for parallel test execution
2. **Resource Pooling**: Reuse containers across tests when safe
3. **Mock Services**: More comprehensive mocking for external services
4. **Test Reporting**: Enhanced test reporting and metrics

---

This guide serves as the authoritative reference for Watchtower E2E testing implementation. Update this document as the framework evolves and new testing patterns are established.
