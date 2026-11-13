package selfupdate

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
)

// RecoveryClient finds and starts an orphaned replacement container.
type RecoveryClient interface {
	// ListLabeledContainers returns the containers in every state whose
	// label is set to "true".
	ListLabeledContainers(ctx context.Context, label string) ([]*container.Container, error)
	// StartContainerByID starts an existing container.
	StartContainerByID(ctx context.Context, containerID container.ContainerID) error
}

// RestartPolicySetter changes a container's restart policy.
type RestartPolicySetter interface {
	// SetRestartPolicy sets the container's restart policy. An empty container
	// ID does nothing.
	SetRestartPolicy(
		ctx context.Context,
		containerID container.ContainerID,
		policy dockerContainer.RestartPolicy,
	) error
}

// RetireClient recovers an orphaned replacement and changes the restart policy
// of an old Watchtower container.
type RetireClient interface {
	RecoveryClient
	RestartPolicySetter
}

// RetireOldInstance stops an old Watchtower container from running again.
//
// It starts an orphaned replacement left in the created state, if there is
// one, and sets the old container's restart policy to "no". Each step runs
// under its own timeout and keeps ctx's values but not its cancellation.
//
// Parameters:
//   - log: Process logger.
//   - ctx: The caller's context.
//   - client: Container client for the recovery and the restart policy.
//   - oldContainer: The old Watchtower container.
//   - timeout: The bound of each step.
func RetireOldInstance(
	log *zerolog.Logger,
	ctx context.Context,
	client RetireClient,
	oldContainer *container.Container,
	timeout time.Duration,
) {
	log.Debug().Str("container", oldContainer.Name()).Msg("Retiring old Watchtower container")

	recoverCtx, recoverCancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	tryRecoverOrphanedContainer(log, recoverCtx, client, oldContainer)

	recoverCancel()

	policyCtx, policyCancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	DisableOldRestarts(log, policyCtx, oldContainer, client)

	policyCancel()
}

// tryRecoverOrphanedContainer attempts to start an orphaned Watchtower container
// that is stuck in the Docker "created" state. This can happen when a self-update
// creates a replacement container but fails to start it, leaving the old container
// running and the new container never started.
//
// It lists the Watchtower containers in every state, whatever the
// include-stopped configuration option says, since the replacement it looks
// for was created but never started. It starts the first container that
// [container.IsOrphanedReplacement] reports as an orphaned replacement of the
// current container and that starts without error. A recovery is logged once,
// at info. A start that fails is logged at error, since no Watchtower may run
// once the old instance exits.
//
// Parameters:
//   - log: Process logger. Required and must be non-nil. A nil logger panics on the first log call.
//   - ctx: Context for cancellation and timeouts.
//   - client: Container client for Docker API operations.
//   - currentContainer: The current running container to exclude from recovery.
func tryRecoverOrphanedContainer(
	log *zerolog.Logger,
	ctx context.Context,
	client RecoveryClient,
	currentContainer *container.Container,
) {
	candidates, err := client.ListLabeledContainers(ctx, labels.Watchtower)
	if err != nil {
		log.Debug().
			Err(err).
			Msg("Failed to list containers for orphaned Watchtower recovery")

		return
	}

	for _, c := range candidates {
		if !container.IsOrphanedReplacement(c, currentContainer) {
			continue
		}

		log.Debug().
			Str("container", c.Name()).
			Str("id", c.ID().ShortID()).
			Msg("Attempting to recover orphaned Watchtower container")

		err := client.StartContainerByID(ctx, c.ID())
		if err != nil {
			log.Error().
				Err(err).
				Str("container", c.Name()).
				Str("id", c.ID().ShortID()).
				Msg(logging.OrphanStartFailed)

			continue
		}

		log.Info().
			Str("container", c.Name()).
			Str("id", c.ID().ShortID()).
			Msg(logging.OrphanRecovered)

		return
	}
}

// DisableOldRestarts sets the restart policy of an old Watchtower container to
// "no", so the runtime does not start it again.
//
// A failure is logged at debug only. An old Watchtower container that starts
// again detects that it is old and exits, and the instance that replaced it
// removes it.
//
// Parameters:
//   - log: Logger for the failure.
//   - ctx: Context for the restart policy update.
//   - oldContainer: The old Watchtower container.
//   - client: Container client for Docker operations.
func DisableOldRestarts(
	log *zerolog.Logger,
	ctx context.Context,
	oldContainer *container.Container,
	client RestartPolicySetter,
) {
	err := client.SetRestartPolicy(
		ctx,
		oldContainer.ID(),
		dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled},
	)
	if err != nil {
		log.Debug().
			Err(err).
			Str("container", oldContainer.Name()).
			Msg("Failed to disable the restart policy of the old Watchtower container")
	}
}
