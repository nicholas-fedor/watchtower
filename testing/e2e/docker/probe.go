package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/testing/e2e/assert"
)

// maxInspectBytes caps inspect JSON and exec output kept in memory.
const maxInspectBytes = 1 << 20

// InspectSnapshot captures docker inspect JSON and the fidelity subset.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - id: Container ID or name.
//
// Returns:
//   - []byte: Raw inspect JSON.
//   - assert.InspectSnapshot: Compared fields.
//   - error: API or parse error.
func InspectSnapshot(ctx context.Context, cli *client.Client, id string) ([]byte, assert.InspectSnapshot, error) {
	view, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, assert.InspectSnapshot{}, fmt.Errorf("inspect %s: %w", id, err)
	}

	raw := []byte(view.Raw)
	if len(raw) > maxInspectBytes {
		raw = raw[:maxInspectBytes]
	}

	snap, parseErr := assert.ParseInspect(raw)
	if parseErr != nil {
		return raw, assert.InspectSnapshot{}, fmt.Errorf("parse inspect: %w", parseErr)
	}

	return raw, snap, nil
}

// ExecInner runs a command in an inner container (HTTP probe, curl).
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Inner Docker client.
//   - containerID: Target container.
//   - cmd: Command.
//
// Returns:
//   - string: stdout.
//   - error: Exec failure.
func ExecInner(ctx context.Context, cli *client.Client, containerID string, cmd []string) (string, error) {
	created, err := cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("exec create: %w", err)
	}

	attached, attachErr := cli.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if attachErr != nil {
		return "", fmt.Errorf("exec attach: %w", attachErr)
	}
	defer attached.Close()

	var buf bytes.Buffer

	_, copyErr := io.Copy(&buf, io.LimitReader(attached.Reader, maxInspectBytes))
	if copyErr != nil {
		return "", fmt.Errorf("exec read: %w", copyErr)
	}

	return buf.String(), nil
}
