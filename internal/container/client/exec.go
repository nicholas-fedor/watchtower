package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// maxExecOutputSize is the maximum captured exec stdout/stderr size (1 MiB).
const maxExecOutputSize = 1 << 20

// Errors for executing commands in containers.
var (
	// errCreateExecFailed indicates a failure to create an exec instance in a container.
	errCreateExecFailed = errors.New("failed to create exec instance")
	// errStartExecFailed indicates a failure to start an exec instance in a container.
	errStartExecFailed = errors.New("failed to start exec instance")
	// errAttachExecFailed indicates a failure to attach to an exec instance for output capture.
	errAttachExecFailed = errors.New("failed to attach to exec instance")
	// errReadExecOutputFailed indicates a failure to read output from an exec instance.
	errReadExecOutputFailed = errors.New("failed to read exec output")
	// errInspectExecFailed indicates a failure to inspect an exec instance's status.
	errInspectExecFailed = errors.New("failed to inspect exec instance")
	// errCommandFailed indicates a command executed in a container failed with a non-zero exit code.
	errCommandFailed = errors.New("command execution failed")
)

// ExecuteCommand runs a command inside a container and evaluates its result.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - container: Container to execute command in.
//   - command: Command to execute.
//   - timeout: Minutes to wait before timeout (0 for no timeout).
//   - uid: UID to run command as (-1 to use container default).
//   - gid: GID to run command as (-1 to use container default).
//
// Returns:
//   - bool: True if updates should be skipped, false otherwise.
//   - error: Non-nil if execution fails, nil on success.
func (c *client) ExecuteCommand(
	ctx context.Context,
	container types.Container,
	command string,
	timeout int,
	uid int,
	gid int,
) (bool, error) {
	clogVal := c.logger().With().
		Str("container_id", string(container.ID())).
		Logger()
	clog := &clogVal

	// Generate JSON metadata for the container.
	metadataJSON, err := generateContainerMetadata(container)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to generate container metadata")

		return false, err
	}

	// Set User if UID or GID are specified (non-zero).
	var user string

	switch {
	case uid > 0 && gid > 0:
		user = fmt.Sprintf("%d:%d", uid, gid)
	case uid > 0:
		user = strconv.Itoa(uid)
	case gid > 0:
		user = fmt.Sprintf(":%d", gid)
	}

	if user != "" {
		clog.Debug().
			Str("user", user).
			Msg("Setting exec user")
	}

	// Set up exec configuration with command and metadata.
	clog.Debug().
		Str("command", command).
		Msg("Creating exec instance")
	execConfig := dockerClient.ExecCreateOptions{
		TTY:          true,
		Cmd:          []string{"sh", "-c", command},
		Env:          []string{"WT_CONTAINER=" + metadataJSON},
		User:         user,
		AttachStdout: true,
		AttachStderr: true,
	}

	// Create the exec instance using the parent context.
	exec, err := c.api.ExecCreate(
		ctx,
		string(container.ID()),
		execConfig,
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to create exec instance")

		return false, fmt.Errorf("%w: %w", errCreateExecFailed, err)
	}

	// Start the exec instance.
	clog.Debug().
		Str("exec_id", exec.ID).
		Msg("Starting exec instance")

	execStartCheck := dockerClient.ExecStartOptions{Detach: true, TTY: true}

	_, err = c.api.ExecStart(ctx, exec.ID, execStartCheck)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to start exec instance")

		return false, fmt.Errorf("%w: %w", errStartExecFailed, err)
	}

	// The timeout bounds the whole run of the command, so a command that never
	// exits stops both reading its output and waiting for its exit code.
	execCtx := ctx

	if timeout > 0 {
		var cancel context.CancelFunc

		execCtx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Minute)
		defer cancel()
	}

	// Capture output and handle attachment.
	output, err := c.captureExecOutput(execCtx, exec.ID)
	if err != nil {
		clog.Warn().
			Err(err).
			Msg("Failed to capture command output")
	}

	// Wait for completion and evaluate result.
	skipUpdate, err := c.waitForExec(execCtx, exec.ID, output)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to inspect exec instance")

		return true, fmt.Errorf("%w: %w", errInspectExecFailed, err)
	}

	clog.Debug().
		Str("command", command).
		Str("output", output).
		Bool("skip_update", skipUpdate).
		Msg("Executed command")

	return skipUpdate, nil
}

// captureExecOutput attaches to an exec instance and captures its output.
//
// Parameters:
//   - ctx: Context for lifecycle control.
//   - execID: ID of the exec instance.
//
// Returns:
//   - string: Captured output if successful.
//   - error: Non-nil if attachment or reading fails, nil on success.
func (c *client) captureExecOutput(ctx context.Context, execID string) (string, error) {
	clogVal := c.logger().With().
		Str("exec_id", execID).
		Logger()
	clog := &clogVal

	// Attach to the exec instance for output.
	clog.Debug().Msg("Attaching to exec instance")

	response, err := c.api.ExecAttach(
		ctx,
		execID,
		dockerClient.ExecAttachOptions{TTY: true},
	)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to attach to exec instance")

		return "", fmt.Errorf("%w: %w", errAttachExecFailed, err)
	}

	defer response.Close()

	// Read output into a buffer with timeout and a size cap.
	var writer bytes.Buffer

	done := make(chan error, 1)

	go func() {
		_, err := io.Copy(
			&writer,
			io.LimitReader(
				response.Reader,
				maxExecOutputSize,
			),
		)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			clog.Debug().
				Err(err).
				Msg("Failed to read exec output")

			return "", fmt.Errorf("%w: %w", errReadExecOutputFailed, err)
		}
	case <-ctx.Done():
		response.Close()

		return "", fmt.Errorf("%w: %w", errReadExecOutputFailed, ctx.Err())
	}

	// Return trimmed output if any was captured.
	if writer.Len() > 0 {
		output := strings.TrimSpace(writer.String())
		clog.Debug().
			Str("output", output).
			Msg("Captured exec output")

		return output, nil
	}

	return "", nil
}

// waitForExec waits for an exec instance to complete, or for ctx to end.
//
// Parameters:
//   - ctx: Context bounding the wait, including the command's timeout.
//   - execID: ID of the exec instance.
//   - execOutput: Captured output for error reporting.
//
// Returns:
//   - bool: True if updates should be skipped (exit code 75), false otherwise.
//   - error: Non-nil if inspection fails, the command errors, or ctx ends first.
func (c *client) waitForExec(
	ctx context.Context,
	execID string,
	execOutput string,
) (bool, error) {
	const ExTempFail = 75

	clogVal := c.logger().With().
		Str("exec_id", execID).
		Logger()
	clog := &clogVal

	// Poll exec status until completion.
	for {
		execInspect, err := c.api.ExecInspect(ctx, execID, dockerClient.ExecInspectOptions{})
		if err != nil {
			clog.Debug().
				Err(err).
				Msg("Failed to inspect exec instance")

			return false, fmt.Errorf("%w: %w", errInspectExecFailed, err)
		}

		clog.Debug().
			Int("exit_code", execInspect.ExitCode).
			Bool("running", execInspect.Running).
			Msg("Checked exec status")

		if execInspect.Running {
			select {
			case <-time.After(1 * time.Second):
				continue
			case <-ctx.Done():
				return false, fmt.Errorf("exec canceled: %w", ctx.Err())
			}
		}

		// Log output if present.
		if len(execOutput) > 0 {
			clog.Debug().
				Int("output_length", len(execOutput)).
				Msg("Command output captured")
		}

		// Handle specific exit codes.
		if execInspect.ExitCode == ExTempFail {
			return true, nil // Skip updates on temporary failure.
		}

		if execInspect.ExitCode > 0 {
			err := fmt.Errorf(
				"%w with exit code %d: %s",
				errCommandFailed,
				execInspect.ExitCode,
				execOutput,
			)
			clog.Debug().
				Err(err).
				Msg("Command execution failed")

			return false, err
		}

		break
	}

	return false, nil
}

// generateContainerMetadata creates a JSON-formatted string of container metadata.
//
// Parameters:
//   - container: Container object to extract metadata from.
//
// Returns:
//   - string: JSON string containing metadata (e.g., name, ID, image name, stop signal, labels).
//   - error: Non-nil if JSON marshaling fails, nil otherwise.
func generateContainerMetadata(container types.Container) (string, error) {
	// Filter Watchtower-specific labels to reduce JSON size
	labels := make(map[string]string)

	containerInfo := container.ContainerInfo()
	if containerInfo != nil &&
		containerInfo.Config != nil {
		for key, value := range containerInfo.Config.Labels {
			if strings.HasPrefix(key, "com.centurylinklabs.watchtower.") {
				labels[key] = value
			}
		}
	}

	metadata := struct {
		Name       string            `json:"name"`
		ID         string            `json:"id"`
		ImageName  string            `json:"image_name"`
		StopSignal string            `json:"stop_signal"`
		Labels     map[string]string `json:"labels"`
	}{
		Name:       container.Name(),
		ID:         string(container.ID()),
		ImageName:  container.ImageName(),
		StopSignal: container.StopSignal(),
		Labels:     labels,
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("failed to marshal container metadata: %w", err)
	}

	return string(metadataJSON), nil
}
