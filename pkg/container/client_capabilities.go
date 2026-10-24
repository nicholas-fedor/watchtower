package container

import (
	"context"
	"fmt"

	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ CopyFileStore = (*client)(nil)

// BuildRemoteImage builds an image from a Git URL context.
//
// The Docker daemon clones the repository. Watchtower does not.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - remote: Docker Git context URL.
//   - dockerfile: Dockerfile path relative to that context. Empty means Dockerfile.
//   - tags: Image tags to apply.
//
// Returns:
//   - types.ImageID: Built image ID.
//   - error: Non-nil when the build or inspect fails.
func (c *client) BuildRemoteImage(ctx context.Context, remote, dockerfile string, tags []string) (types.ImageID, error) {
	imageID, err := containerImage.BuildFromGit(c.logger(), ctx, c.api, remote, dockerfile, tags)
	if err != nil {
		return "", fmt.Errorf("build image from Git: %w", err)
	}

	return imageID, nil
}

// SnapshotCopyFiles captures labeled files before the source is removed.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - source: Source container about to be stopped and removed.
//
// Returns:
//   - error: Non-nil if a labeled path cannot be snapshotted.
func (c *client) SnapshotCopyFiles(ctx context.Context, source types.Container) error {
	snapshot, err := snapshotCopyFiles(c.logger(), ctx, c.api, source)
	if err != nil {
		return err
	}

	if snapshot.empty() {
		return nil
	}

	return c.copyFiles.store(source.ID(), snapshot)
}

// injectStoredCopyFiles writes a stored snapshot into a newly created container.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - source: Source container whose snapshot should be injected.
//   - newID: ID of the replacement container.
//
// Returns:
//   - error: Non-nil if a stored file cannot be written into the new container.
func (c *client) injectStoredCopyFiles(
	ctx context.Context,
	source types.Container,
	newID types.ContainerID,
) error {
	snapshot, ok := c.copyFiles.take(source.ID())
	if !ok {
		return nil
	}

	return injectCopyFiles(c.logger(), ctx, c.api, string(newID), source, snapshot)
}

// DiscardCopyFiles drops a leftover snapshot for the source container.
//
// Parameters:
//   - containerID: Source container ID whose snapshot should be discarded.
func (c *client) DiscardCopyFiles(containerID types.ContainerID) {
	c.copyFiles.take(containerID)
}

// CreateEphemeralOrchestrator creates a short-lived container that orchestrates
// the Watchtower self-update transition.
//
// The ephemeral container uses the same Watchtower image (already pulled) with
// the --self-update-orchestrator flag. It is configured with AutoRemove for
// automatic cleanup and mounts the Docker socket for container management.
//
// The ephemeral container does not set the watchtower label
// (com.centurylinklabs.watchtower = "true") to avoid being detected as an
// excess Watchtower instance by the scope and filter systems.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - sourceContainer: Current Watchtower container being replaced.
//   - newImage: Image reference for the new Watchtower container.
//   - containerChain: Container chain label for lineage tracking.
//   - cleanup: When true, the orchestrator removes the old image after handoff.
//
// Returns:
//   - types.ContainerID: ID of the ephemeral orchestrator container.
//   - error: Non-nil if creation or start fails, nil on success.
func (c *client) CreateEphemeralOrchestrator(
	ctx context.Context,
	sourceContainer types.Container,
	newImage string,
	containerChain string,
	cleanup bool,
) (types.ContainerID, error) {
	return createEphemeralOrchestrator(c.log, ctx, c.api, sourceContainer, newImage, containerChain, cleanup)
}
