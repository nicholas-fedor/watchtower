package watchtower

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	containerTypes "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/testing/e2e/docker"
	"github.com/nicholas-fedor/watchtower/testing/e2e/engine"
)

const (
	// watchtowerImage is the thin Watchtower image tag inside DinD.
	watchtowerImage = "e2e/watchtower:local"
	// runOnceTimeout is how long WaitRunOnce waits for Watchtower to exit.
	runOnceTimeout = 2 * time.Minute
)

// Instance is a running Watchtower process or container.
type Instance struct {
	// Name is the inner container name when packaging is container.
	Name string
	// ID is the inner container ID.
	ID string
	// Cmd is the host process when packaging is binary.
	Cmd *exec.Cmd
	// Stdout is captured stdout.
	Stdout io.Writer
	// Stderr is captured stderr.
	Stderr io.Writer
	// logsDone is closed when container log copy finishes.
	logsDone <-chan struct{}
}

// Start launches Watchtower against the inner daemon.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner DinD worker.
//   - artifacts: Built binary and subject.
//   - item: Case vector.
//   - caseDir: Artifact directory for this case.
//   - extraHosts: extra_hosts for persona hijack.
//
// Returns:
//   - *Instance: Running Watchtower.
//   - []string: Argv used.
//   - map[string]string: Env used.
//   - error: Start failure.
func Start(
	ctx context.Context,
	daemon *docker.Daemon,
	artifacts Artifacts,
	item engine.Case,
	stdout, stderr io.WriteCloser,
	extraHosts []string,
) (*Instance, []string, map[string]string, error) {
	cfg := item.Watchtower
	cfg.ApplyObservability()

	if item.Shape == engine.ShapeRunOnce && cfg.Porcelain == nil {
		cfg.Porcelain = new("json")
	}

	args, env := cfg.Render(item.Channel)
	args = append(args, item.Names...)

	if stdout == nil || stderr == nil {
		return nil, nil, nil, fmt.Errorf("watchtower logs: writers required")
	}

	if item.Packaging == engine.PackagingBinary {
		inst, startErr := startBinary(ctx, daemon, artifacts.Binary, args, env, stdout, stderr)

		return inst, args, env, startErr
	}

	inst, startErr := startContainer(ctx, daemon, artifacts, args, env, extraHosts, stdout, stderr, item)

	return inst, args, env, startErr
}

// startBinary launches the host-built Watchtower binary against the inner daemon.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner DinD worker.
//   - binary: Path to the Watchtower executable.
//   - args: CLI arguments.
//   - env: Extra environment variables.
//   - stdout: Stdout log file.
//   - stderr: Stderr log file.
//
// Returns:
//   - *Instance: Running process.
//   - error: Start failure.
func startBinary(
	ctx context.Context,
	daemon *docker.Daemon,
	binary string,
	args []string,
	env map[string]string,
	stdout, stderr io.Writer,
) (*Instance, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()

	cmd.Env = append(cmd.Env, "DOCKER_HOST="+daemon.Host())
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	err := cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("start watchtower binary: %w", err)
	}

	return &Instance{Cmd: cmd, Stdout: stdout, Stderr: stderr}, nil
}

// startContainer launches Watchtower as a container inside DinD.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner DinD worker.
//   - artifacts: Built Watchtower binary.
//   - args: CLI arguments.
//   - env: Extra environment variables.
//   - extraHosts: extra_hosts for persona hijack.
//   - stdout: Stdout log file.
//   - stderr: Stderr log file.
//   - item: Case vector for envelopes.
//
// Returns:
//   - *Instance: Running container.
//   - error: Image build or start failure.
func startContainer(
	ctx context.Context,
	daemon *docker.Daemon,
	artifacts Artifacts,
	args []string,
	env map[string]string,
	extraHosts []string,
	stdout, stderr io.Writer,
	item engine.Case,
) (*Instance, error) {
	loadErr := ensureWatchtowerImage(ctx, daemon, artifacts)
	if loadErr != nil {
		return nil, loadErr
	}

	envList := make([]string, 0, len(env)+1)

	envList = append(envList, "DOCKER_HOST=unix:///var/run/docker.sock")
	for key, value := range env {
		envList = append(envList, key+"="+value)
	}

	host := &containerTypes.HostConfig{
		Binds:      []string{"/var/run/docker.sock:/var/run/docker.sock"},
		ExtraHosts: extraHosts,
	}
	if item.Topology.WatchtowerEnvelope.MemoryBytes > 0 {
		host.Memory = item.Topology.WatchtowerEnvelope.MemoryBytes
		host.MemorySwap = item.Topology.WatchtowerEnvelope.MemoryBytes
	}

	if item.Topology.WatchtowerEnvelope.NanoCPUs > 0 {
		host.NanoCPUs = item.Topology.WatchtowerEnvelope.NanoCPUs
	}

	name := "e2e-watchtower"

	apiPort, apiPortErr := network.ParsePort("8080/tcp")
	if apiPortErr != nil {
		return nil, fmt.Errorf("api port: %w", apiPortErr)
	}

	created, err := daemon.Client().ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &containerTypes.Config{
			Image:        watchtowerImage,
			Cmd:          args,
			Env:          envList,
			ExposedPorts: network.PortSet{apiPort: {}},
		},
		HostConfig: host,
		Name:       name,
	})
	if err != nil {
		return nil, fmt.Errorf("create watchtower container: %w", err)
	}

	_, startErr := daemon.Client().ContainerStart(ctx, created.ID, client.ContainerStartOptions{})
	if startErr != nil {
		return nil, fmt.Errorf("start watchtower container: %w", startErr)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)

		copyLogs(ctx, daemon.Client(), created.ID, stdout, stderr)
	}()

	return &Instance{Name: name, ID: created.ID, Stdout: stdout, Stderr: stderr, logsDone: done}, nil
}

// ensureWatchtowerImage builds the thin scratch Watchtower image on the inner daemon.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner DinD worker.
//   - artifacts: Host-built Watchtower binary.
//
// Returns:
//   - error: Build failure.
func ensureWatchtowerImage(ctx context.Context, daemon *docker.Daemon, artifacts Artifacts) error {
	if docker.HasImage(ctx, daemon.Client(), watchtowerImage) {
		return nil
	}

	dockerfile := docker.WatchtowerDockerfile()

	tarStream, err := docker.ContextTar(dockerfile, "watchtower", artifacts.Binary)
	if err != nil {
		return fmt.Errorf("watchtower image tar: %w", err)
	}

	build, buildErr := daemon.Client().ImageBuild(ctx, tarStream, client.ImageBuildOptions{
		Tags:       []string{watchtowerImage},
		Dockerfile: "Dockerfile",
		Remove:     true,
	})
	if buildErr != nil {
		return fmt.Errorf("build watchtower image: %w", buildErr)
	}
	defer build.Body.Close()

	_, _ = io.Copy(io.Discard, build.Body)

	return nil
}

// copyLogs follows container logs into the case stdout file.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - id: Watchtower container ID.
//   - stdout: Destination for stdout.
//   - stderr: Destination for stderr.
func copyLogs(ctx context.Context, cli *client.Client, id string, stdout, stderr io.Writer) {
	logs, err := cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return
	}
	defer logs.Close()

	_, _ = stdcopy.StdCopy(stdout, stderr, logs)
}

// WaitRunOnce waits for a run-once process or container to exit.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner daemon (container packaging).
//   - inst: Started instance.
//
// Returns:
//   - int: Exit code.
//   - error: Timeout or wait failure.
func WaitRunOnce(ctx context.Context, daemon *docker.Daemon, inst *Instance) (int, error) {
	waitCtx, cancel := context.WithTimeout(ctx, runOnceTimeout)
	defer cancel()

	if inst.Cmd != nil {
		done := make(chan error, 1)
		go func() {
			done <- inst.Cmd.Wait()
		}()

		select {
		case err := <-done:
			if err != nil {
				return 1, err
			}

			return 0, nil
		case <-waitCtx.Done():
			_ = inst.Cmd.Process.Kill()

			return -1, fmt.Errorf("watchtower run-once timed out: %w", waitCtx.Err())
		}
	}

	waited := daemon.Client().ContainerWait(waitCtx, inst.ID, client.ContainerWaitOptions{
		Condition: containerTypes.WaitConditionNotRunning,
	})
	select {
	case result := <-waited.Result:
		waitLogs(waitCtx, inst)

		return int(result.StatusCode), nil
	case err := <-waited.Error:
		return -1, fmt.Errorf("wait watchtower: %w", err)
	case <-waitCtx.Done():
		return -1, fmt.Errorf("watchtower run-once timed out: %w", waitCtx.Err())
	}
}

// waitLogs waits until container log copy has finished or ctx is done.
//
// Parameters:
//   - ctx: Cancellation.
//   - inst: Started instance.
func waitLogs(ctx context.Context, inst *Instance) {
	if inst.logsDone == nil {
		return
	}

	select {
	case <-inst.logsDone:
	case <-ctx.Done():
	}
}

// Close releases log files and stops a leftover container.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner daemon.
func (i *Instance) Close(ctx context.Context, daemon *docker.Daemon) {
	if i == nil {
		return
	}

	if closer, ok := i.Stdout.(io.Closer); ok {
		_ = closer.Close()
	}

	if closer, ok := i.Stderr.(io.Closer); ok {
		_ = closer.Close()
	}

	if i.ID != "" && daemon != nil {
		_, _ = daemon.Client().ContainerRemove(ctx, i.ID, client.ContainerRemoveOptions{Force: true})
	}
}
