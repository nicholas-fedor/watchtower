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

// RenameClient renames the running Watchtower container for a rename-based
// self-update and undoes the rename when its replacement fails.
type RenameClient interface {
	Renamer
	ReplacementRemover
	RestartPolicySetter
}

// Renamer gives a container a new name.
type Renamer interface {
	// RenameContainer gives the container a new name.
	RenameContainer(ctx context.Context, target *container.Container, newName string) error
}

// ReplacementRemover removes a Watchtower replacement that failed to start.
type ReplacementRemover interface {
	// GetContainer returns the container with the given ID.
	GetContainer(ctx context.Context, containerID container.ContainerID) (*container.Container, error)
	// StopAndRemoveContainer stops the container and removes it. The stop
	// waits up to timeout.
	StopAndRemoveContainer(ctx context.Context, target *container.Container, timeout time.Duration) error
}

// Rename is a rename-based self-update of the running Watchtower container.
//
// The running container takes its old name, so its replacement can be created
// under the original name while this process keeps running. The zero value is
// a restart that renamed nothing, and its methods do nothing.
type Rename struct {
	// log names the container and its image.
	log *zerolog.Logger
	// source is the running Watchtower container.
	source *container.Container
	// originalName is the name the rename freed. It is empty when the
	// container already had its old name.
	originalName string
	// renamed is true while the container is under its old name.
	renamed bool
}

// errNameNotRestored indicates that a failed self-update left Watchtower under its "watchtower-old-" name.
var errNameNotRestored = errors.New("could not restore Watchtower's container name")

// BeginRename renames the running Watchtower container to its old name and
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
//   - Rename: The self-update, with the container under its old name.
//   - error: The client's error when the rename fails.
func BeginRename(
	log *zerolog.Logger,
	ctx context.Context,
	client Renamer,
	source *container.Container,
) (Rename, error) {
	clogVal := log.With().
		Str("container", source.Name()).
		Str("image", source.ImageName()).
		Logger()
	clog := &clogVal

	renamedNow, err := renameToOldName(clog, ctx, client, source)
	if err != nil {
		return Rename{}, err
	}

	rename := Rename{log: clog, source: source, originalName: "", renamed: true}
	if renamedNow {
		rename.originalName = source.Name()
	}

	// The replacement is created from the source's configuration, so it
	// carries the chain that names this container as its predecessor.
	chain := source.SuccessorChain()
	source.SetLabel(labels.ContainerChain, chain)
	clog.Debug().
		Str("container_chain", chain).
		Msg("Updated container chain label for Watchtower self-update")

	return rename, nil
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
func (r *Rename) Abort(
	ctx context.Context,
	client RenameClient,
	newContainerID container.ContainerID,
	config updateParams.Params,
	cause error,
) error {
	if !r.renamed {
		return cause
	}

	removeFailedReplacement(r.log, ctx, client, r.source, newContainerID, config)

	if r.originalName == "" {
		return cause
	}

	renameCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.DetachedTimeout())
	defer cancel()

	oldName := r.source.OldName()

	err := client.RenameContainer(renameCtx, r.source, r.originalName)
	if err != nil {
		return fmt.Errorf("%w, and %w %s from %s: %w", cause, errNameNotRestored, r.originalName, oldName, err)
	}

	r.renamed = false

	r.log.Debug().
		Str("old_name", oldName).
		Msg("Restored Watchtower container name after a failed self-update")

	return cause
}

// Renamed reports whether the running container is under its old name.
//
// Returns:
//   - bool: True while the container is under its old name.
func (r *Rename) Renamed() bool {
	return r.renamed
}

// Retire sets the restart policy of the container under its old name to "no"
// once its replacement runs, so the runtime does not start it again.
//
// Parameters:
//   - ctx: Context for the restart policy update.
//   - client: Container client for the restart policy.
func (r *Rename) Retire(ctx context.Context, client RestartPolicySetter) {
	if !r.renamed {
		return
	}

	r.log.Debug().Msg("Updating restart policy for old Watchtower container")

	DisableOldRestarts(r.log, ctx, r.source, client)
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
//   - bool: True when this call renamed the container.
//   - error: The client's error when the rename fails.
func renameToOldName(
	log *zerolog.Logger,
	ctx context.Context,
	client Renamer,
	c *container.Container,
) (bool, error) {
	oldName := c.OldName()

	if container.IsOldContainer(c.Name()) {
		log.Debug().
			Str("old_name", oldName).
			Msg("Skipping rename of a Watchtower container that already has its old name")

		return false, nil
	}

	err := client.RenameContainer(ctx, c, oldName)
	if err != nil {
		return false, err //nolint:wrapcheck // The client's error already names the failed rename.
	}

	log.Debug().
		Str("old_name", oldName).
		Msg("Renamed Watchtower container to its old name")

	return true, nil
}

// removeFailedReplacement stops and removes a Watchtower replacement that
// failed to start, so its predecessor can take the original name back.
//
// It does nothing for an empty container ID. The removal keeps ctx's values
// but not its cancellation or deadline, and gets the stop timeout plus 30
// seconds, using the source's own stop timeout when it is longer, since the
// replacement copies it. A failure is logged at debug only.
//
// Parameters:
//   - log: Logger that names the self-update.
//   - ctx: The self-update's context.
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

	clog.Debug().Msg("Removing failed Watchtower replacement")

	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		config.DetachedStopTimeout(source),
	)
	defer cancel()

	failedNew, err := client.GetContainer(cleanupCtx, newContainerID)
	if err != nil {
		clog.Debug().
			Err(err).
			Msg("Failed to inspect failed Watchtower replacement")

		return
	}

	err = client.StopAndRemoveContainer(cleanupCtx, failedNew, config.Timeout)
	if err != nil && !cerrdefs.IsNotFound(err) {
		clog.Debug().
			Err(err).
			Msg("Failed to remove failed Watchtower replacement")
	}
}
