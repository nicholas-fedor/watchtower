package rebuild

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/git"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Check runs a Git staleness check and stores the result.
//
// An invalid git-semver-policy or git-host label logs a warning, because the
// update skips that container and nothing else reports it above debug. The
// warning leaves out the raw git-host value, so a credential in that label is
// not notified.
//
// Parameters:
//   - log: Logger for the warning about an invalid Git label.
//   - ctx: Cancellation and timeout.
//   - c: Associated container.
//   - params: Update parameters including cooldown.
//
// Returns:
//   - bool: True when the remote ref has advanced past the known running revision.
//   - types.ImageID: Synthetic Git image id used by the update pipeline.
//   - error: Non-nil on monitor or cooldown failure.
func (s *Session) Check(
	log *zerolog.Logger,
	ctx context.Context,
	c types.Container,
	params types.UpdateParams,
) (bool, types.ImageID, error) {
	result, err := git.CheckContainer(ctx, s.client, c, params)
	if err != nil {
		var warning string

		switch {
		case errors.Is(err, git.ErrInvalidPolicy):
			warning = "Skipped container with an invalid git semver policy"
		case errors.Is(err, git.ErrInvalidHost):
			warning = "Skipped container with an invalid git-host"
		default:
			// Other check failures are only returned.
		}

		if warning != "" {
			log.Warn().Str("container", c.Name()).Str("image", c.ImageName()).Msg(warning)
		}

		return false, "", fmt.Errorf("git check: %w", err)
	}

	s.store(c.ID(), result)

	if !result.Stale {
		return false, c.ImageID(), nil
	}

	err = containerImage.CheckLocalImageCooldown(c, params)
	if err != nil {
		return false, "", fmt.Errorf("git cooldown: %w", err)
	}

	newest := types.ImageID("git:" + result.Commit)
	if newest == "git:" {
		newest = types.ImageID("git:unknown")
	}

	return true, newest, nil
}
