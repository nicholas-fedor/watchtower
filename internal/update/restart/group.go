package restart

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/rs/zerolog"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/nicholas-fedor/watchtower/internal/container"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	"github.com/nicholas-fedor/watchtower/internal/update/selfupdate"
)

// Errors for replacing a container that others join.
var (
	// errNotRestored indicates that a container whose replacement failed could not be restored.
	errNotRestored = errors.New("could not restore the container")
	// errRebindNotStopped indicates that a container joining another's network namespace could not be stopped.
	errRebindNotStopped = errors.New("could not stop a container that joins its network")
	// errGroupKept indicates that a container was left as it was because the container whose network its group
	// joins was not replaced.
	errGroupKept = errors.New("left in place with the container whose network its group joins")
	// errReplacementKept indicates that a container whose removal failed could not be inspected, so its
	// replacement was kept.
	errReplacementKept = errors.New("could not tell whether the container was removed, so its replacement was kept")
)

// networkGroups splits containers in restart order into groups.
//
// Each group starts with a container and runs through the last rebound
// container that joins its network namespace or that of another member. A
// rebound container placed after a container it waits for keeps its group,
// which then also holds the containers in between. A rebound container whose
// network container is not in the list starts a group of its own.
//
// Parameters:
//   - containers: Containers in restart order.
//
// Returns:
//   - [][]*container.Container: The groups, in restart order.
func networkGroups(containers []*container.Container) [][]*container.Container {
	// starts holds the index of each group's first container.
	starts := make([]int, 0, len(containers))

	for i, c := range containers {
		joined := -1
		if c.IsRebinding() {
			joined = slices.IndexFunc(containers[:i], c.JoinsNetworkOf)
		}

		if joined < 0 {
			starts = append(starts, i)

			continue
		}

		// The groups after the one holding the joined container merge into it.
		for starts[len(starts)-1] > joined {
			starts = starts[:len(starts)-1]
		}
	}

	groups := make([][]*container.Container, len(starts))

	for i, start := range starts {
		end := len(containers)
		if i+1 < len(starts) {
			end = starts[i+1]
		}

		groups[i] = containers[start:end]
	}

	return groups
}

// stopGroup stops a container and the containers after it in its group and
// records the outcomes.
//
// A container that others join is replaced before they are stopped, unless
// its host config names one of the earlier containers. That one is recreated
// after the group stops. A replacement created now would stay bound to the
// removed one. Otherwise each member stops on its own, in reverse order, until
// a member that joins another member cannot be stopped. The members before it
// then stay as they are and are skipped, and a running one that could not be
// stopped starts again.
//
// Parameters:
//   - log: Session logger.
//   - ctx: Context for cancellation and timeouts.
//   - group: The container, followed by the rest of its group.
//   - earlier: The containers before the group that are not recreated yet.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and other behaviors.
//   - failed: Receives the error of each container that failed.
//   - stopped: Receives each container that was stopped and removed with the
//     replacement already created for it, empty when none was created.
func stopGroup(
	log *zerolog.Logger,
	ctx context.Context,
	group, earlier []*container.Container,
	client Client,
	config updateParams.Params,
	failed map[container.ContainerID]error,
	stopped map[container.ContainerID]container.ContainerID,
) {
	head := group[0]

	if len(group) > 1 && head.ToRestart() && len(head.CreateConfigTargets(earlier)) == 0 {
		stopNetworkGroup(log, ctx, group, client, config, failed, stopped)

		return
	}

	// The container comes last in reverse order, and it joins no member, so
	// it never keeps the group in place.
	stopMembers(log, ctx, group, group, client, config, failed, stopped)
}

// stopNetworkGroup replaces a container and stops the rest of its group.
//
// The members that do not reach the container only through rebound
// containers stop first, in reverse order, each right after its pre-update
// command. Watchtower recreates them in any case, so they stop as they would
// without the replacement. The container's own pre-update command runs after
// them. The container then steps aside under a temporary name so its
// replacement can be created under its own name. Only then are the rebound
// containers that reach it stopped, in reverse order, followed by the
// container. From then on each stop runs to its end even when the update is
// canceled, since Docker finishes a stop it has begun. A container gone before
// it steps aside is skipped, while one gone after its replacement exists
// counts as removed.
//
// A member that joins another member and cannot be stopped before the
// replacement exists leaves the container in place. When one of these steps
// fails after the replacement exists, the replacement is undone:
//   - stopping a member that joins another member
//   - removing the container, when the container still exists
//
// A container the daemon removed although its removal failed counts as
// removed. When it cannot be inspected either, the replacement is kept and the
// container fails, since removing the replacement could leave neither.
//
// The replacement is then removed. The container takes its name back and
// starts again if it was running. Members already removed are recreated
// against it. A member that could not be stopped starts again if it was
// running, and the members not reached yet are skipped. A member that joins
// no other member and cannot be stopped fails on its own.
//
// Parameters:
//   - log: Session logger.
//   - ctx: Context for cancellation and timeouts.
//   - group: The container, followed by the rest of its group.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and other behaviors.
//   - failed: Receives the error of each container that failed.
//   - stopped: Receives each container that was stopped and removed, with the
//     replacement already created for the container others join.
func stopNetworkGroup(
	log *zerolog.Logger,
	ctx context.Context,
	group []*container.Container,
	client Client,
	config updateParams.Params,
	failed map[container.ContainerID]error,
	stopped map[container.ContainerID]container.ContainerID,
) {
	head := group[0]
	reaching, others := splitReaching(group)

	clogVal := log.With().
		Str("container", head.Name()).
		Str("image", head.ImageName()).
		Logger()
	clog := &clogVal

	if !stopMembers(log, ctx, group, others, client, config, failed, stopped) {
		return
	}

	// The container steps aside only while the update runs, since nothing
	// after that point is cut short.
	if ctx.Err() != nil {
		skipCanceled(log, ctx, group, failed, stopped)

		return
	}

	clog.Debug().Msg("Stopping container for restart")

	err := prepareStop(log, ctx, head, client, config)
	if err != nil {
		failed[head.ID()] = err
		keepMembers(group, failed, stopped)

		return
	}

	// Podman refuses to remove a container whose namespace others join, so
	// they are removed first. The container keeps running under another name
	// until its replacement exists, so a failure can still restore it.
	// A container gone by now is skipped with the rest of its group left
	// alone.
	err = client.RenameContainer(ctx, head, head.ReplacedName())
	if err != nil {
		failed[head.ID()] = abandonStop(log, client, head, err, "Failed to rename container aside for its replacement")
		keepMembers(group, failed, stopped)

		return
	}

	createCtx, cancelCreate := context.WithTimeout(context.WithoutCancel(ctx), config.CreateStartTimeout())
	defer cancelCreate()

	newID, err := client.CreateContainer(createCtx, head)
	if err != nil {
		clog.Debug().Err(err).Msg("Failed to create container")

		failed[head.ID()] = restoreReplaced(clog, ctx, client, head, "", nil, config, err)
		keepMembers(group, failed, stopped)

		return
	}

	for _, member := range slices.Backward(reaching) {
		// A container still joined to the old namespace would lose its
		// network with it, so the replacement is undone instead. Once the
		// replacement exists, the group finishes or is restored even when the
		// update is canceled. The stop bounds itself, and this bound covers
		// the steps before it, such as the copy-file snapshot.
		prepareCtx, cancelPrepare := context.WithTimeout(context.WithoutCancel(ctx), config.DetachedTimeout())
		err = stopOne(log, prepareCtx, member, client, config, failed, stopped)

		cancelPrepare()

		if !holdsMember(group, member, err) {
			continue
		}

		heldBy(group, member, err, failed, stopped)
		failed[head.ID()] = restoreReplaced(clog, ctx, client, head, newID, reaching, config, failed[head.ID()])

		// The member starts again only once the namespace it joins is restored.
		startAgain(ctx, member, client, config, failed, err)

		return
	}

	// A container already gone counts as removed, since its replacement
	// exists. So does one the daemon removed although the removal failed,
	// such as when the removal outlasted its bound.
	err = stopAndRemove(log, ctx, head, client, config)
	if err != nil && !errors.Is(err, errContainerGone) {
		exists, inspectErr := stillExists(ctx, client, head, config)

		switch {
		case inspectErr != nil:
			// Removing the replacement could leave neither container, so
			// it stays for the container to be recovered by hand.
			failed[head.ID()] = fmt.Errorf("%w: %w", errReplacementKept, errors.Join(err, inspectErr))

			return
		case exists:
			failed[head.ID()] = restoreReplaced(clog, ctx, client, head, newID, reaching, config, err)

			return
		default:
		}
	}

	stopped[head.ID()] = newID

	clog.Debug().Msg("Stopped container")
}

// restoreReplaced undoes the replacement of a container that steps aside for
// it, so the container keeps its name and runs as before.
//
// After the replacement is removed, the container takes its name back. It is
// started when it was running, under its temporary name when the rename
// fails. The joiners that name it then name it by ID, so those recreated
// against it still find it. These steps keep ctx's values but not its
// cancellation. The rename and the start get the stop timeout plus 30
// seconds. A failed removal shows as a failed rename.
//
// Parameters:
//   - log: Logger that names the container.
//   - ctx: The update's context.
//   - client: Container client for Docker operations.
//   - source: The container that stepped aside.
//   - newID: The replacement, empty when it was not created.
//   - joiners: The containers that may be recreated against source.
//   - config: Update options. The removal waits up to the stop timeout.
//   - cause: The error that failed the replacement.
//
// Returns:
//   - error: cause, joined with the steps that failed when the container
//     could not be restored.
func restoreReplaced(
	log *zerolog.Logger,
	ctx context.Context,
	client Client,
	source *container.Container,
	newID container.ContainerID,
	joiners []*container.Container,
	config updateParams.Params,
	cause error,
) error {
	selfupdate.RemoveFailedReplacement(log, ctx, client, source, newID, config)

	restoreCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.DetachedTimeout())
	defer cancel()

	var restoreErrs []error

	err := client.RenameContainer(restoreCtx, source, source.Name())
	if err != nil {
		restoreErrs = append(restoreErrs, err)

		for _, joiner := range joiners {
			if joiner.JoinsNetworkOf(source) {
				joiner.SetNetworkContainer(string(source.ID()))
			}
		}
	}

	// Starting a container that still runs does nothing. A container left
	// under its temporary name is started too, so it keeps running.
	if source.IsRunning() {
		err = client.StartContainerByID(restoreCtx, source.ID())
		if err != nil {
			restoreErrs = append(restoreErrs, err)
		}
	}

	if len(restoreErrs) > 0 {
		return fmt.Errorf("%w, and %w: %w", cause, errNotRestored, errors.Join(restoreErrs...))
	}

	log.Debug().Msg("Restored container after its replacement failed")

	return cause
}

// stillExists reports whether a container whose removal failed still exists.
//
// The inspect keeps ctx's values but not its cancellation and gets the stop
// timeout plus 30 seconds.
//
// Parameters:
//   - ctx: The update's context.
//   - client: Container client for Docker operations.
//   - c: The container whose removal failed.
//   - config: Update options holding the stop timeout.
//
// Returns:
//   - bool: True when the daemon still has c.
//   - error: The inspect's error when it cannot tell, nil otherwise.
func stillExists(ctx context.Context, client Client, c *container.Container, config updateParams.Params) (bool, error) {
	inspectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.DetachedTimeout())
	defer cancel()

	_, err := client.GetContainer(inspectCtx, c.ID())

	switch {
	case err == nil:
		return true, nil
	case cerrdefs.IsNotFound(err):
		return false, nil
	default:
		return false, err //nolint:wrapcheck // The client's error already names the failed inspect.
	}
}

// stopMembers stops members of a group in reverse order.
//
// A member that joins another member and cannot be stopped keeps the group in
// place. The group's first container then fails and the members not stopped
// yet are skipped. The member starts again if it was running. Once the update
// is canceled, no further member is stopped, and the group's containers not
// stopped yet are skipped.
//
// Parameters:
//   - log: Session logger.
//   - ctx: Context for cancellation and timeouts.
//   - group: The container, followed by the rest of its group.
//   - members: The members to stop, in group order.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and other behaviors.
//   - failed: Receives the error of each container that failed or was
//     skipped.
//   - stopped: Receives each member that was stopped and removed.
//
// Returns:
//   - bool: False when a member kept the group in place.
func stopMembers(
	log *zerolog.Logger,
	ctx context.Context,
	group, members []*container.Container,
	client Client,
	config updateParams.Params,
	failed map[container.ContainerID]error,
	stopped map[container.ContainerID]container.ContainerID,
) bool {
	for _, member := range slices.Backward(members) {
		if ctx.Err() != nil {
			skipCanceled(log, ctx, group, failed, stopped)

			return false
		}

		err := stopOne(log, ctx, member, client, config, failed, stopped)
		if holdsMember(group, member, err) {
			heldBy(group, member, err, failed, stopped)
			startAgain(ctx, member, client, config, failed, err)

			return false
		}
	}

	return true
}

// skipCanceled skips the containers a canceled update did not stop.
//
// Parameters:
//   - log: Session logger.
//   - ctx: The canceled context.
//   - containers: The containers to skip.
//   - failed: Receives the skip of each container.
//   - stopped: The containers stopped and removed so far.
func skipCanceled(
	log *zerolog.Logger,
	ctx context.Context,
	containers []*container.Container,
	failed map[container.ContainerID]error,
	stopped map[container.ContainerID]container.ContainerID,
) {
	for _, c := range containers {
		if settled(c, failed, stopped) {
			continue
		}

		log.Info().
			Str("container", c.Name()).
			Str("image", c.ImageName()).
			Str("container_id", c.ID().ShortID()).
			Msg("Skipped container stop due to context cancellation")

		failed[c.ID()] = skipped(fmt.Errorf("stop skipped: %w", ctx.Err()))
	}
}

// stopOne stops a single container, without the rest of its group, and
// records the outcome.
//
// Parameters:
//   - log: Session logger.
//   - ctx: Context for cancellation and timeouts.
//   - c: Container to stop.
//   - client: Container client for Docker operations.
//   - config: Update options specifying stop timeout and other behaviors.
//   - failed: Receives the error when the stop fails.
//   - stopped: Receives the container when it was stopped and removed.
//
// Returns:
//   - error: The error recorded in failed, nil when none was.
func stopOne(
	log *zerolog.Logger,
	ctx context.Context,
	c *container.Container,
	client Client,
	config updateParams.Params,
	failed map[container.ContainerID]error,
	stopped map[container.ContainerID]container.ContainerID,
) error {
	stoppedNow, err := stopStaleContainer(log, ctx, c, client, config)
	if err != nil {
		failed[c.ID()] = err

		return err
	}

	if stoppedNow {
		stopped[c.ID()] = ""

		log.Debug().
			Str("container", c.Name()).
			Str("image", c.ImageName()).
			Msg("Stopped container")
	}

	return nil
}

// holdsMember reports whether a container still holds the network namespace
// of a member of group after its stop returned err.
//
// A container that could not be stopped keeps running in the namespace it
// joins. One already gone holds none.
//
// Parameters:
//   - group: The members whose namespaces count.
//   - c: The container that was to be stopped.
//   - err: The stop's error, nil when it succeeded.
//
// Returns:
//   - bool: True when c was not stopped and joins a member of group.
func holdsMember(group []*container.Container, c *container.Container, err error) bool {
	return err != nil && !errors.Is(err, errContainerGone) && slices.ContainsFunc(group, c.JoinsNetworkOf)
}

// heldBy records that a member which could not be stopped keeps the rest of
// its group in place.
//
// The member keeps running in the namespace it joins. The group's first
// container fails with the member's error. A rebound member is skipped, since
// it keeps its network and that failure reports its error. The members not
// stopped yet are skipped.
//
// Parameters:
//   - group: The container, followed by the rest of its group.
//   - member: The member that could not be stopped.
//   - err: The member's stop error.
//   - failed: Receives the error of each container that failed or was
//     skipped.
//   - stopped: The containers stopped and removed so far.
func heldBy(
	group []*container.Container,
	member *container.Container,
	err error,
	failed map[container.ContainerID]error,
	stopped map[container.ContainerID]container.ContainerID,
) {
	// The member's error is kept as text, so a skip of the member cannot
	// make the container's failure read as a skip.
	failed[group[0].ID()] = fmt.Errorf("%w: %s: %s", errRebindNotStopped, member.Name(), err.Error())

	if member.IsRebinding() {
		failed[member.ID()] = skipped(fmt.Errorf("%w: %w", errGroupKept, err))
	}

	keepMembers(group, failed, stopped)
}

// keepMembers skips the members of a group that were left as they were.
//
// Parameters:
//   - group: The container, followed by the rest of its group.
//   - failed: Receives the skip of each member left as it was.
//   - stopped: The containers stopped and removed so far.
func keepMembers(
	group []*container.Container,
	failed map[container.ContainerID]error,
	stopped map[container.ContainerID]container.ContainerID,
) {
	for _, member := range group[1:] {
		if !settled(member, failed, stopped) && member.ToRestart() {
			failed[member.ID()] = skipped(errGroupKept)
		}
	}
}

// settled reports whether a container needs no skip for the rest of its group.
//
// A container stopped and removed already is recreated, and one with an
// outcome keeps it.
//
// Parameters:
//   - c: The container.
//   - failed: The error of each container that failed or was skipped.
//   - stopped: The containers stopped and removed so far.
//
// Returns:
//   - bool: True when c was removed or has an outcome.
func settled(
	c *container.Container,
	failed map[container.ContainerID]error,
	stopped map[container.ContainerID]container.ContainerID,
) bool {
	_, removed := stopped[c.ID()]
	_, recorded := failed[c.ID()]

	return removed || recorded
}

// splitReaching splits the members of a group by how they reach its first
// container.
//
// A rebound member that joins the first container, or another member that
// does, reaches it. The other members include the containers placed in the
// group between them and the rebound containers that join one of those.
//
// Parameters:
//   - group: The container, followed by the rest of its group.
//
// Returns:
//   - []*container.Container: The rebound members that reach the first
//     container, in group order.
//   - []*container.Container: The other members, in group order.
//
//nolint:nonamedreturns // Same-type returns need names.
func splitReaching(group []*container.Container) (reaching, others []*container.Container) {
	reached := []*container.Container{group[0]}

	for _, member := range group[1:] {
		if member.IsRebinding() && slices.ContainsFunc(reached, member.JoinsNetworkOf) {
			reached = append(reached, member)
			reaching = append(reaching, member)

			continue
		}

		others = append(others, member)
	}

	return reaching, others
}

// startAgain starts a container whose stop failed when it was running.
//
// It may have stopped before its removal failed. Starting one that still runs
// does nothing. A paused container is left as it is, since Docker and Podman
// refuse to stop or start one. The start keeps ctx's values but not its
// cancellation and gets the stop timeout plus 30 seconds.
//
// Parameters:
//   - ctx: The update's context.
//   - c: The container that could not be stopped.
//   - client: Container client for Docker operations.
//   - config: Update options holding the stop timeout.
//   - failed: Receives c's stop error joined with the start's when the start
//     fails.
//   - cause: The error that kept c from being stopped.
func startAgain(
	ctx context.Context,
	c *container.Container,
	client Client,
	config updateParams.Params,
	failed map[container.ContainerID]error,
	cause error,
) {
	if !c.IsRunning() || c.IsPaused() {
		return
	}

	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.DetachedTimeout())
	defer cancel()

	err := client.StartContainerByID(startCtx, c.ID())
	if err != nil {
		failed[c.ID()] = errors.Join(cause, err)
	}
}
