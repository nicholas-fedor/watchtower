package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/dind"

	containerTypes "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

const (
	// DinDImage is the privileged Docker-in-Docker image.
	DinDImage = "docker:28.0.1-dind"
	// RegistryImage is the distribution v2 image loaded into DinD from the host.
	RegistryImage = "registry:2.8.3"
	// daemonStartTimeout is how long StartDaemon waits for ping.
	daemonStartTimeout = 2 * time.Minute
	// ryukConnectionTimeout is raised so long cartesian sittings outlive the 10s default.
	ryukConnectionTimeout = "5m"
	// ryukReconnectTimeout is the Ryuk reconnect budget.
	ryukReconnectTimeout = "30s"
)

// Daemon is one Testcontainers-managed DinD worker and its inner Docker client.
type Daemon struct {
	// container is the outer Testcontainers DinD handle.
	container *dind.Container
	// cli is the inner Docker API client.
	cli *client.Client
	// host is the inner API URL (tcp://ip:2375).
	host string
}

// ConfigureRyuk sets Testcontainers reaper timeouts for long local runs.
//
// Ryuk stays enabled. Do not set TESTCONTAINERS_RYUK_DISABLED.
func ConfigureRyuk() {
	_ = setDefaultEnv("TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT", ryukConnectionTimeout)
	_ = setDefaultEnv("TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT", ryukReconnectTimeout)
}

// StartDaemon starts a privileged DinD container and pings the inner API.
//
// The Testcontainers DinD module listens on TCP 2375 only. This overrides
// dockerd so it also binds unix:///var/run/docker.sock. Watchtower runs as a
// child of that daemon and mounts that socket, the same way it does on a
// normal Docker host. The harness talks to the inner API over the published
// TCP port. There is no host-daemon fallback.
//
// Parameters:
//   - ctx: Cancellation.
//   - envelope: Optional CPU/memory budget applied to the DinD container.
//
// Returns:
//   - *Daemon: Worker with an inner client.
//   - error: Startup, privilege, or ping failure.
func StartDaemon(ctx context.Context, envelope engine.Envelope) (*Daemon, error) {
	ConfigureRyuk()

	startCtx, cancel := context.WithTimeout(ctx, daemonStartTimeout)
	defer cancel()

	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithCmd(
			"dockerd",
			"-H", "unix:///var/run/docker.sock",
			"-H", "tcp://0.0.0.0:2375",
			"--tls=false",
			"--insecure-registry=127.0.0.1:5000",
		),
		testcontainers.WithHostConfigModifier(func(hostConfig *containerTypes.HostConfig) {
			hostConfig.Privileged = true
			if envelope.MemoryBytes > 0 {
				hostConfig.Memory = envelope.MemoryBytes
				hostConfig.MemorySwap = envelope.MemoryBytes
			}

			if envelope.NanoCPUs > 0 {
				hostConfig.NanoCPUs = envelope.NanoCPUs
			}

			if envelope.PidsLimit > 0 {
				limit := envelope.PidsLimit
				hostConfig.PidsLimit = &limit
			}
		}),
	}

	dindContainer, err := dind.Run(startCtx, DinDImage, opts...)
	if err != nil {
		return nil, fmt.Errorf("start dind (privileged required): %w", err)
	}

	host, hostErr := dindContainer.Host(startCtx)
	if hostErr != nil {
		_ = testcontainers.TerminateContainer(dindContainer)

		return nil, fmt.Errorf("dind host url: %w", hostErr)
	}

	cli, clientErr := client.New(client.WithHost(host))
	if clientErr != nil {
		_ = testcontainers.TerminateContainer(dindContainer)

		return nil, fmt.Errorf("inner docker client: %w", clientErr)
	}

	_, pingErr := cli.Ping(startCtx, client.PingOptions{NegotiateAPIVersion: true})
	if pingErr != nil {
		_ = cli.Close()
		_ = testcontainers.TerminateContainer(dindContainer)

		return nil, fmt.Errorf("inner docker ping (privileged dind required): %w", pingErr)
	}

	daemon := &Daemon{
		container: dindContainer,
		cli:       cli,
		host:      host,
	}

	lockErr := daemon.LockdownEgress(startCtx)
	if lockErr != nil {
		_ = daemon.Close(ctx)

		return nil, lockErr
	}

	return daemon, nil
}

// Host returns the inner Docker API URL (tcp://ip:2375).
//
// Returns:
//   - string: DOCKER_HOST value for Watchtower or a second client.
func (d *Daemon) Host() string {
	if d == nil {
		return ""
	}

	return d.host
}

// Client returns the inner moby client.
//
// Returns:
//   - *client.Client: Inner daemon client.
func (d *Daemon) Client() *client.Client {
	if d == nil {
		return nil
	}

	return d.cli
}

// LoadImage copies a host image into the inner daemon.
//
// Parameters:
//   - ctx: Cancellation.
//   - image: Host image reference that must already exist locally.
//
// Returns:
//   - error: Load failure.
func (d *Daemon) LoadImage(ctx context.Context, image string) error {
	err := d.container.LoadImage(ctx, image)
	if err != nil {
		return fmt.Errorf("load image %s into dind: %w", image, err)
	}

	return nil
}

// Exec runs a command in the DinD container (not an inner container).
//
// Parameters:
//   - ctx: Cancellation.
//   - cmd: Command and arguments.
//
// Returns:
//   - string: Combined output.
//   - error: Exec failure.
func (d *Daemon) Exec(ctx context.Context, cmd []string) (string, error) {
	code, reader, err := d.container.Exec(ctx, cmd)
	if err != nil {
		return "", fmt.Errorf("dind exec: %w", err)
	}

	raw, readErr := io.ReadAll(reader)
	if readErr != nil {
		return "", fmt.Errorf("dind exec output: %w", readErr)
	}

	if code != 0 {
		return string(raw), fmt.Errorf("%w: exit %d: %s", ErrDinDExecFailed, code, strings.TrimSpace(string(raw)))
	}

	return string(raw), nil
}

// LockdownEgress rejects forwarded public 80/443 from inner containers.
//
// Inner-to-inner registry traffic stays allowed. An attempted live Hub pull
// must fail closed.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: When iptables cannot be applied.
func (d *Daemon) LockdownEgress(ctx context.Context) error {
	commands := [][]string{
		{"iptables", "-I", "FORWARD", "-p", "tcp", "--dport", "443", "-j", "REJECT"},
		{"iptables", "-I", "FORWARD", "-p", "tcp", "--dport", "80", "-j", "REJECT"},
		{"iptables", "-I", "FORWARD", "-d", "10.0.0.0/8", "-j", "ACCEPT"},
		{"iptables", "-I", "FORWARD", "-d", "172.16.0.0/12", "-j", "ACCEPT"},
		{"iptables", "-I", "FORWARD", "-d", "192.168.0.0/16", "-j", "ACCEPT"},
		{"iptables", "-I", "FORWARD", "-s", "10.0.0.0/8", "-d", "10.0.0.0/8", "-j", "ACCEPT"},
		{"iptables", "-I", "FORWARD", "-s", "172.16.0.0/12", "-d", "172.16.0.0/12", "-j", "ACCEPT"},
	}

	var lastErr error

	for _, cmd := range commands {
		_, err := d.Exec(ctx, cmd)
		if err != nil {
			lastErr = err
		}
	}

	if lastErr != nil {
		return fmt.Errorf("lockdown egress (iptables inside dind): %w", lastErr)
	}

	return nil
}

// Version returns the inner Docker engine version string.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - string: Server version.
//   - error: API error.
func (d *Daemon) Version(ctx context.Context) (string, error) {
	info, err := d.cli.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return "", fmt.Errorf("docker version: %w", err)
	}

	return info.Version, nil
}

// Reset removes case-prefixed containers, networks, and dangling subjects.
// The inner registry container may stay.
//
// Parameters:
//   - ctx: Cancellation.
//   - prefix: Case resource prefix.
//
// Returns:
//   - error: Cleanup error.
func (d *Daemon) Reset(ctx context.Context, prefix string) error {
	return resetPrefix(ctx, d.cli, prefix)
}

// Close terminates the inner client and the DinD container.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Combined close errors.
func (d *Daemon) Close(ctx context.Context) error {
	var closeErr error
	if d.cli != nil {
		closeErr = d.cli.Close()
	}

	if d.container != nil {
		termErr := testcontainers.TerminateContainer(d.container)
		if termErr != nil {
			return fmt.Errorf("terminate dind: %w", termErr)
		}
	}

	if closeErr != nil {
		return fmt.Errorf("close inner client: %w", closeErr)
	}

	return nil
}

// setDefaultEnv sets key to value when it is not already present in the environment.
//
// Parameters:
//   - key: Environment variable name.
//   - value: Value to store.
//
// Returns:
//   - error: Setenv failure.
func setDefaultEnv(key, value string) error {
	if os.Getenv(key) != "" {
		return nil
	}

	setErr := os.Setenv(key, value)
	if setErr != nil {
		return fmt.Errorf("set %s: %w", key, setErr)
	}

	return nil
}
