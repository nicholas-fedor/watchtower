package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/moby/moby/client"

	containerTypes "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

const (
	// innerRegistryName is the distribution container name inside DinD.
	innerRegistryName = "e2e-distribution"
	// innerRegistryAddr is the loopback host:port dockerd uses to push and pull.
	innerRegistryAddr = "127.0.0.1:5000"
	// registryReadyWait is the poll interval for persona /e2e-control/health.
	registryReadyWait = 200 * time.Millisecond
	// registryReadyTries is how many health polls to attempt.
	registryReadyTries = 25
)

// SubjectPullRef is the image name Watchtower pulls for dummy subjects.
//
// Returns:
//   - string: 127.0.0.1:5000/e2e/app:latest
func SubjectPullRef() string {
	return engine.SubjectImageRef()
}

// StartInnerRegistry runs distribution v2 inside DinD after loading RegistryImage.
//
// The registry is published on the DinD loopback so dockerd can push and pull
// 127.0.0.1:5000/... Watchtower then uses the Docker API, so pulls still go
// through that inner daemon, not the host engine.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Worker whose inner daemon hosts the registry.
//
// Returns:
//   - string: Container ID.
//   - error: Load, create, or start failure.
func StartInnerRegistry(ctx context.Context, daemon *Daemon) (string, error) {
	if !HasImage(ctx, daemon.Client(), RegistryImage) {
		loadErr := daemon.LoadImage(ctx, RegistryImage)
		if loadErr != nil {
			return "", loadErr
		}
	}

	created, err := daemon.Client().ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &containerTypes.Config{
			Image: RegistryImage,
			Env:   []string{"REGISTRY_STORAGE_DELETE_ENABLED=true"},
		},
		Name: innerRegistryName,
	})
	if err != nil {
		if isConflict(err) {
			return innerRegistryName, nil
		}

		return "", fmt.Errorf("create inner registry: %w", err)
	}

	_, startErr := daemon.Client().ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	if startErr != nil {
		return "", fmt.Errorf("start inner registry: %w", startErr)
	}

	netErr := attachToRegistryNet(ctx, daemon.Client(), created.ID)
	if netErr != nil {
		return "", netErr
	}

	return created.ID, nil
}

// RegistryBackendURL returns http://<registry-container-ip>:5000 for the persona proxy.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner DinD worker.
//   - registryID: Registry container ID or name.
//
// Returns:
//   - string: Backend URL.
//   - error: Inspect failure or missing IP.
func RegistryBackendURL(ctx context.Context, daemon *Daemon, registryID string) (string, error) {
	view, err := daemon.Client().ContainerInspect(ctx, registryID, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect registry: %w", err)
	}

	ip := containerIP(view)
	if ip == "" {
		return "", errNoContainerIP
	}

	return "http://" + ip + ":5000", nil
}

// registryNet is the user-defined network shared by distribution and the persona.
const registryNet = "e2e-reg"

// attachToRegistryNet puts a container on the user-defined network so persona can resolve the registry by name.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - containerID: Container to attach.
//
// Returns:
//   - error: Network create or connect failure.
func attachToRegistryNet(ctx context.Context, cli *client.Client, containerID string) error {
	ensureErr := ensureNetwork(ctx, cli, registryNet)
	if ensureErr != nil {
		return ensureErr
	}

	_, connErr := cli.NetworkConnect(ctx, registryNet, client.NetworkConnectOptions{
		Container: containerID,
	})
	if connErr != nil {
		if isConflict(connErr) {
			return nil
		}

		return fmt.Errorf("connect %s to %s: %w", containerID, registryNet, connErr)
	}

	return nil
}

// waitRegistry polls until the inner registry answers on loopback.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: DinD worker.
//
// Returns:
//   - error: Timeout waiting for /v2/.
func waitRegistry(ctx context.Context, daemon *Daemon) error {
	var lastErr error

	for range registryReadyTries {
		_, lastErr = daemon.Exec(ctx, []string{"wget", "-q", "-O", "-", "http://127.0.0.1:5000/e2e-control/health"})
		if lastErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait registry: %w", ctx.Err())
		case <-time.After(registryReadyWait):
		}
	}

	return fmt.Errorf("inner registry not ready: %w", lastErr)
}
