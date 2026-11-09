package rebuild

import (
	"context"
	"fmt"

	"github.com/nicholas-fedor/watchtower/internal/container"
	containerImage "github.com/nicholas-fedor/watchtower/internal/container/image"
	"github.com/nicholas-fedor/watchtower/internal/git"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
)

// Check runs a Git staleness check and stores the result.
//
// An invalid git-semver-policy or git-host label returns an error matching
// [github.com/nicholas-fedor/watchtower/internal/git/semver.ErrInvalidPolicy]
// or [github.com/nicholas-fedor/watchtower/internal/container/provenance.ErrInvalidHost].
// The caller reports it.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - c: Associated container.
//   - params: Update parameters including cooldown.
//
// Returns:
//   - bool: True when the remote ref has advanced past the known running revision.
//   - container.ImageID: Synthetic Git image id used by the update pipeline.
//   - error: Non-nil on monitor or cooldown failure.
func (s *Session) Check(
	ctx context.Context,
	c *container.Container,
	params updateParams.Params,
) (bool, container.ImageID, error) {
	result, err := git.CheckContainer(ctx, s.client, c, params.Git)
	if err != nil {
		return false, "", fmt.Errorf("git check: %w", err)
	}

	s.store(c.ID(), result)

	if !result.Stale {
		return false, c.ImageID(), nil
	}

	err = containerImage.CheckLocalImageCooldown(c, params.Policy)
	if err != nil {
		return false, "", fmt.Errorf("git cooldown: %w", err)
	}

	newest := container.ImageID("git:" + result.Commit)
	if newest == "git:" {
		newest = container.ImageID("git:unknown")
	}

	return true, newest, nil
}
