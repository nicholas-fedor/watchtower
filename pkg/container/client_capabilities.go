package container

import (
	"context"
	"fmt"

	"github.com/nicholas-fedor/watchtower/internal/container/copyfile"
	"github.com/nicholas-fedor/watchtower/internal/container/ephemeral"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// CopyFileStore captures labeled files around recreate and discards leftovers.
type CopyFileStore interface {
	// SnapshotCopyFiles captures labeled files before the source is removed.
	SnapshotCopyFiles(ctx context.Context, container types.Container) error
	// DiscardCopyFiles drops a leftover snapshot for the source container.
	DiscardCopyFiles(containerID types.ContainerID)
}

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
	snapshot, err := copyfile.Capture(c.logger(), ctx, c.api, source)
	if err != nil {
		return fmt.Errorf("capture copy-file snapshot: %w", err)
	}

	if snapshot.Empty() {
		return nil
	}

	err = c.copyFiles.Store(source.ID(), snapshot)
	if err != nil {
		return fmt.Errorf("store copy-file snapshot: %w", err)
	}

	return nil
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
	snapshot, ok := c.copyFiles.Take(source.ID())
	if !ok {
		return nil
	}

	err := copyfile.Inject(c.logger(), ctx, c.api, string(newID), source, snapshot)
	if err != nil {
		return fmt.Errorf("inject copy files: %w", err)
	}

	return nil
}

// DiscardCopyFiles drops a leftover snapshot for the source container.
//
// Parameters:
//   - containerID: Source container ID whose snapshot should be discarded.
func (c *client) DiscardCopyFiles(containerID types.ContainerID) {
	c.copyFiles.Take(containerID)
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
	orchestratorID, err := ephemeral.CreateOrchestrator(c.log, ctx, c.api, sourceContainer, newImage, containerChain, cleanup)
	if err != nil {
		return "", fmt.Errorf("create ephemeral orchestrator: %w", err)
	}

	return orchestratorID, nil
}
