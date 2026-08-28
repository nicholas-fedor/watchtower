package docker

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/moby/moby/client"

	containerTypes "github.com/moby/moby/api/types/container"
	networkTypes "github.com/moby/moby/api/types/network"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

const (
	// watchtowerEnableLabel is com.centurylinklabs.watchtower.enable.
	watchtowerEnableLabel = "com.centurylinklabs.watchtower.enable"
	// watchtowerScopeLabel is com.centurylinklabs.watchtower.scope.
	watchtowerScopeLabel = "com.centurylinklabs.watchtower.scope"
	// dependsOnLabel is com.centurylinklabs.watchtower.depends-on.
	dependsOnLabel = "com.centurylinklabs.watchtower.depends-on"
	// stopSignalLabel is com.centurylinklabs.watchtower.stop-signal.
	stopSignalLabel = "com.centurylinklabs.watchtower.stop-signal"
	// monitorOnlyLabel is the per-container monitor-only label.
	monitorOnlyLabel = "com.centurylinklabs.watchtower.monitor-only"
	// noPullLabel is the per-container no-pull label.
	noPullLabel = "com.centurylinklabs.watchtower.no-pull"
	// cooldownLabel is the per-container cooldown label.
	cooldownLabel = "com.centurylinklabs.watchtower.cooldown-period"
	// preCheckLabel is the lifecycle pre-check label.
	preCheckLabel = "com.centurylinklabs.watchtower.lifecycle.pre-check"
	// postCheckLabel is the lifecycle post-check label.
	postCheckLabel = "com.centurylinklabs.watchtower.lifecycle.post-check"
	// preUpdateLabel is the lifecycle pre-update label.
	preUpdateLabel = "com.centurylinklabs.watchtower.lifecycle.pre-update"
	// postUpdateLabel is the lifecycle post-update label.
	postUpdateLabel = "com.centurylinklabs.watchtower.lifecycle.post-update"
	// preTimeoutLabel is the lifecycle pre-update-timeout label.
	preTimeoutLabel = "com.centurylinklabs.watchtower.lifecycle.pre-update-timeout"
	// subjectHTTPPort is the dummy subject's listen port.
	subjectHTTPPort = "8080/tcp"
)

// CreateSubject starts one subject container on the inner daemon.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - name: Container name (already prefixed).
//   - image: Image reference.
//   - topo: Topology for labels, networks, state.
//
// Returns:
//   - string: Container ID.
//   - error: Create or start failure.
func CreateSubject(ctx context.Context, cli *client.Client, name, image string, topo engine.Topology) (string, error) {
	labels := map[string]string{}
	maps.Copy(labels, topo.Labels)

	if topo.EnableLabel != "" {
		labels[watchtowerEnableLabel] = topo.EnableLabel
	}

	if topo.ScopeLabel != "" {
		labels[watchtowerScopeLabel] = topo.ScopeLabel
	}

	if topo.StopSignal != "" {
		labels[stopSignalLabel] = topo.StopSignal
	}

	if topo.MonitorOnlyLabel != "" {
		labels[monitorOnlyLabel] = topo.MonitorOnlyLabel
	}

	if topo.NoPullLabel != "" {
		labels[noPullLabel] = topo.NoPullLabel
	}

	if topo.CooldownLabel != "" {
		labels[cooldownLabel] = topo.CooldownLabel
	}

	applyLifecycleLabels(labels, topo.Lifecycle)

	cfg := &containerTypes.Config{
		Image:  image,
		Labels: labels,
		Env:    append([]string{}, topo.ExtraEnv...),
	}

	host := &containerTypes.HostConfig{}
	if topo.RestartPolicy != "" {
		host.RestartPolicy = containerTypes.RestartPolicy{Name: containerTypes.RestartPolicyMode(topo.RestartPolicy)}
	}

	if topo.SubjectEnvelope.MemoryBytes > 0 {
		host.Memory = topo.SubjectEnvelope.MemoryBytes
		host.MemorySwap = topo.SubjectEnvelope.MemoryBytes
	}

	if topo.SubjectEnvelope.NanoCPUs > 0 {
		host.NanoCPUs = topo.SubjectEnvelope.NanoCPUs
	}

	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     cfg,
		HostConfig: host,
		Name:       name,
	})
	if err != nil {
		return "", fmt.Errorf("create subject %s: %w", name, err)
	}

	if topo.SubjectState != "created" {
		_, startErr := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
		if startErr != nil {
			return "", fmt.Errorf("start subject %s: %w", name, startErr)
		}
	}

	if topo.SubjectState == "exited" {
		stopTimeout := 1

		_, stopErr := cli.ContainerStop(ctx, created.ID, client.ContainerStopOptions{Timeout: &stopTimeout})
		if stopErr != nil {
			return "", fmt.Errorf("stop subject %s: %w", name, stopErr)
		}
	}

	if topo.SubjectState == "paused" {
		_, pauseErr := cli.ContainerPause(ctx, created.ID, client.ContainerPauseOptions{})
		if pauseErr != nil {
			return "", fmt.Errorf("pause subject %s: %w", name, pauseErr)
		}
	}

	connectErr := connectNetworks(ctx, cli, created.ID, topo.Networks)
	if connectErr != nil {
		return "", connectErr
	}

	return created.ID, nil
}

// applyLifecycleLabels copies hook commands onto Watchtower lifecycle labels.
//
// Parameters:
//   - labels: Container labels to mutate.
//   - life: Hook commands and timeouts.
func applyLifecycleLabels(labels map[string]string, life engine.LifecycleTopo) {
	if life.PreCheck != "" {
		labels[preCheckLabel] = life.PreCheck
	}

	if life.PostCheck != "" {
		labels[postCheckLabel] = life.PostCheck
	}

	if life.PreUpdate != "" {
		labels[preUpdateLabel] = life.PreUpdate
	}

	if life.PostUpdate != "" {
		labels[postUpdateLabel] = life.PostUpdate
	}

	if life.PreTimeout != "" {
		labels[preTimeoutLabel] = life.PreTimeout
	}
}

// connectNetworks attaches a container to extra inner networks, creating them if needed.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - containerID: Subject container ID.
//   - networks: Network names.
//
// Returns:
//   - error: Create or connect failure.
func connectNetworks(ctx context.Context, cli *client.Client, containerID string, networks []string) error {
	for _, name := range networks {
		ensureErr := ensureNetwork(ctx, cli, name)
		if ensureErr != nil {
			return ensureErr
		}

		_, connErr := cli.NetworkConnect(ctx, name, client.NetworkConnectOptions{
			Container:      containerID,
			EndpointConfig: &networkTypes.EndpointSettings{},
		})
		if connErr != nil {
			return fmt.Errorf("connect %s to %s: %w", containerID, name, connErr)
		}
	}

	return nil
}

// ensureNetwork creates an inner network, ignoring already-exists errors.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - name: Network name.
//
// Returns:
//   - error: Create failure other than conflict.
func ensureNetwork(ctx context.Context, cli *client.Client, name string) error {
	_, err := cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{})
	if err != nil {
		if isConflict(err) {
			return nil
		}

		return fmt.Errorf("create network %s: %w", name, err)
	}

	return nil
}

// isConflict reports whether err is a Docker already-exists conflict.
//
// Parameters:
//   - err: API error.
//
// Returns:
//   - bool: True when the resource already exists.
func isConflict(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	return strings.Contains(msg, "already exists") || strings.Contains(msg, "Conflict")
}
