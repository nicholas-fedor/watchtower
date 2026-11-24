package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// PredecessorClient renames the running Watchtower container for a
// rename-based self-update, undoes the rename when its replacement fails and
// disables its restarts once the replacement runs.
type PredecessorClient interface {
	RollbackClient
	RestartPolicySetter
}

// RollbackClient removes a failed replacement and gives the container it was
// created from its name back.
type RollbackClient interface {
	Renamer
	ReplacementRemover
}

// Renamer gives a container a new name.
type Renamer interface {
	// RenameContainer gives the container a new name.
	RenameContainer(ctx context.Context, target *container.Container, newName string) error
}

// ReplacementRemover removes a replacement that failed.
type ReplacementRemover interface {
	// GetContainer returns the container with the given ID.
	GetContainer(ctx context.Context, containerID container.ContainerID) (*container.Container, error)
	// StopAndRemoveContainer stops the container and removes it. The stop
	// waits up to timeout.
	StopAndRemoveContainer(ctx context.Context, target *container.Container, timeout time.Duration) error
}

// Predecessor is the running Watchtower container during a rename-based
// self-update.
//
// The running container takes its old name, so its replacement can be created
// under the original name while this process keeps running. Its methods do
// nothing on the zero value, which renamed nothing.
type Predecessor struct {
	// log names the container and its image.
	log *zerolog.Logger
	// rename is the running Watchtower container's rename to its old name.
	rename TemporaryRename
}

// TemporaryRename is a container's rename to a temporary name, so its
// replacement can be created under the container's own name.
//
// Its methods do nothing on the zero value, which renamed nothing.
type TemporaryRename struct {
	// source is the renamed container.
	source *container.Container
	// originalName is the name the rename freed. It is empty when the
	// container already had its temporary name, so a rollback keeps it.
	originalName string
	// renamed is true while the container is under its temporary name.
	renamed bool
}

// errNameNotRestored indicates that a failed self-update left Watchtower under its "watchtower-old-" name.
var errNameNotRestored = errors.New("could not restore Watchtower's container name")

// NewPredecessor renames the running Watchtower container to its old name and
// adds it to the container chain its replacement is created with.
//
// A container that already has its old name, left by an earlier self-update,
// is not renamed again.
//
// Parameters:
//   - log: Process logger.
//   - ctx: Context for the rename.
//   - client: Container client for the rename.
//   - source: The running Watchtower container.
//
// Returns:
//   - Predecessor: The container, under its old name.
//   - error: The client's error when the rename fails.
func NewPredecessor(
	log *zerolog.Logger,
	ctx context.Context,
	client Renamer,
	source *container.Container,
) (Predecessor, error) {
	clogVal := log.With().
		Str("container", source.Name()).
		Str("image", source.ImageName()).
		Logger()
	clog := &clogVal

	rename, err := renameToOldName(clog, ctx, client, source)
	if err != nil {
		return Predecessor{}, err
	}

	// The replacement is created from the source's configuration, so it
	// carries the chain that names this container as its predecessor.
	chain := source.SuccessorChain()
	source.SetLabel(labels.ContainerChain, chain)
	clog.Debug().
		Str("container_chain", chain).
		Msg("Updated container chain label for Watchtower self-update")

	return Predecessor{log: clog, rename: rename}, nil
}

// Abort undoes the rename after the replacement failed to be created or
// started, so the running container keeps the original name.
//
// A replacement that was created holds the original name, so it is removed
// first. The rename back then runs under the detached timeout, keeping ctx's
// values but not its cancellation or deadline, since the self-update may have
// run out of time. An instance left under its old name stops at its next
// update check or restart, so a failed rename back is added to the returned
// error, which the update reports once with the container. A restored name
// logs only at debug.
//
// Parameters:
//   - ctx: The self-update's context.
//   - client: Container client for the removal and the rename back.
//   - newContainerID: The failed replacement, or empty when it was not created.
//   - config: Update options. The removal waits up to the stop timeout.
//   - cause: The error that failed the self-update.
//
// Returns:
//   - error: cause, joined with the failed rename when the name was not restored.
func (p *Predecessor) Abort(
	ctx context.Context,
	client PredecessorClient,
	newContainerID container.ContainerID,
	config updateParams.Params,
	cause error,
) error {
	if !p.HasOldName() {
		return cause
	}

	err := p.rename.Rollback(p.log, ctx, client, newContainerID, config)
	if err != nil {
		return fmt.Errorf("%w, and %w %s from %s: %w",
			cause, errNameNotRestored, p.rename.originalName, p.rename.source.OldWatchtowerName(), err)
	}

	// A container that already had its old name keeps it.
	if p.HasOldName() {
		return cause
	}

	p.log.Debug().
		Str("old_name", p.rename.source.OldWatchtowerName()).
		Msg("Restored Watchtower container name after a failed self-update")

	return cause
}

// HasOldName reports whether the running container has its old name.
//
// Returns:
//   - bool: True while the container has its old name.
func (p *Predecessor) HasOldName() bool {
	return p.rename.HasTemporaryName()
}

// Retire sets the restart policy of the container under its old name to "no"
// once its replacement runs, so the runtime does not start it again.
//
// Parameters:
//   - ctx: Context for the restart policy update.
//   - client: Container client for the restart policy.
func (p *Predecessor) Retire(ctx context.Context, client RestartPolicySetter) {
	if !p.HasOldName() {
		return
	}

	p.log.Debug().Msg("Updating restart policy for old Watchtower container")

	DisableOldRestarts(p.log, ctx, p.rename.source, client)
}

// RenameForReplacement renames a container to a temporary name, so its
// replacement can be created under the container's own name.
//
// Parameters:
//   - ctx: Context for the rename.
//   - client: Container client for the rename.
//   - source: The container to replace.
//   - tempName: The name the container takes while its replacement is made.
//
// Returns:
//   - TemporaryRename: The rename, with the container under its temporary name.
//   - error: The client's error when the rename fails.
func RenameForReplacement(
	ctx context.Context,
	client Renamer,
	source *container.Container,
	tempName string,
) (TemporaryRename, error) {
	err := client.RenameContainer(ctx, source, tempName)
	if err != nil {
		return TemporaryRename{}, err //nolint:wrapcheck // The client's error already names the failed rename.
	}

	return TemporaryRename{source: source, originalName: source.Name(), renamed: true}, nil
}

// Rollback undoes the rename after the replacement failed to be created or
// started, so the container takes its name back.
//
// A replacement that was created holds the container's name, so it is
// removed first. Since the update may have run out of time, the rename back
// keeps ctx's values without its cancellation or deadline and gets the stop
// timeout plus 30 seconds. A container that already had its temporary name
// keeps it.
//
// Parameters:
//   - log: Logger that names the container.
//   - ctx: The update's context.
//   - client: Container client for the removal and the rename back.
//   - newContainerID: The failed replacement, or empty when it was not
//     created.
//   - config: Update options. The removal waits up to the stop timeout.
//
// Returns:
//   - error: The client's error when the rename back fails, nil otherwise.
func (tr *TemporaryRename) Rollback(
	log *zerolog.Logger,
	ctx context.Context,
	client RollbackClient,
	newContainerID container.ContainerID,
	config updateParams.Params,
) error {
	if !tr.renamed {
		return nil
	}

	removeFailedReplacement(log, ctx, client, tr.source, newContainerID, config)

	if tr.originalName == "" {
		return nil
	}

	renameCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.DetachedTimeout())
	defer cancel()

	err := client.RenameContainer(renameCtx, tr.source, tr.originalName)
	if err != nil {
		return err //nolint:wrapcheck // The client's error already names the failed rename.
	}

	tr.renamed = false

	return nil
}

// HasTemporaryName reports whether the container has its temporary name.
//
// Returns:
//   - bool: True while the container has its temporary name.
func (tr *TemporaryRename) HasTemporaryName() bool {
	return tr.renamed
}

// renameToOldName gives a Watchtower container its old name, so a replacement
// can take its original name.
//
// A container that already has its old name, left by an earlier self-update,
// is not renamed again.
//
// Parameters:
//   - log: Logger that names the container.
//   - ctx: Context for the rename.
//   - client: Container client for the rename.
//   - c: The Watchtower container to rename.
//
// Returns:
//   - TemporaryRename: The container's rename to its old name.
//   - error: The client's error when the rename fails.
func renameToOldName(
	log *zerolog.Logger,
	ctx context.Context,
	client Renamer,
	c *container.Container,
) (TemporaryRename, error) {
	oldName := c.OldWatchtowerName()

	if container.IsOldContainer(c.Name()) {
		log.Debug().
			Str("old_name", oldName).
			Msg("Skipping rename of a Watchtower container that already has its old name")

		// A rollback leaves the container under the old name it already had.
		return TemporaryRename{source: c, originalName: "", renamed: true}, nil
	}

	rename, err := RenameForReplacement(ctx, client, c, oldName)
	if err != nil {
		return rename, err
	}

	log.Debug().
		Str("old_name", oldName).
		Msg("Renamed Watchtower container to its old name")

	return rename, nil
}

// removeFailedReplacement stops and removes a replacement that failed, so the
// container it was created from can take its name back.
//
// It does nothing for an empty container ID. The removal keeps ctx's values
// but not its cancellation or deadline, and gets the stop timeout plus 30
// seconds, using the source's own stop timeout when it is longer, since the
// replacement copies it. A replacement already gone counts as removed. A
// failure is logged at debug only.
//
// Parameters:
//   - log: Logger that names the source container.
//   - ctx: The update's context.
//   - client: Container client for the removal.
//   - source: The container the replacement was created from.
//   - newContainerID: ID of the failed replacement.
//   - config: Update options. The stop waits up to the stop timeout.
func removeFailedReplacement(
	log *zerolog.Logger,
	ctx context.Context,
	client ReplacementRemover,
	source *container.Container,
	newContainerID container.ContainerID,
	config updateParams.Params,
) {
	if newContainerID == "" {
		return
	}

	clogVal := log.With().Str("new_id", newContainerID.ShortID()).Logger()
	clog := &clogVal

	clog.Debug().Msg("Removing failed replacement")

	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		config.DetachedStopTimeout(source),
	)
	defer cancel()

	failedNew, err := client.GetContainer(cleanupCtx, newContainerID)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to inspect failed replacement")

		return
	}

	err = client.StopAndRemoveContainer(cleanupCtx, failedNew, config.Timeout)
	if err != nil && !cerrdefs.IsNotFound(err) {
		clog.Debug().
			Err(err).
			Msg("Failed to remove failed replacement")
	}
}
