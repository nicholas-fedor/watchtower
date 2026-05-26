package container

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/util"
	"github.com/nicholas-fedor/watchtower/pkg/registry"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// CooldownError carries the human-readable cooldown details (age, delay,
// remaining) plus whether the window ultimately passed. It wraps
// ErrImageCooldown (or a fetch failure) so callers can use errors.Is/As
// while still extracting the fields needed for progress reports and
// rich notifications.
type CooldownError struct {
	Age       string
	Delay     string
	Remaining string
	Passed    bool
	err       error
}

func (e *CooldownError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}

	return ErrImageCooldown.Error()
}

func (e *CooldownError) Unwrap() error { return e.err }

func (e *CooldownError) Is(target error) bool {
	return errors.Is(e.err, target) || target == ErrImageCooldown
}

// isOutsideCooldown reports whether the container's image is outside its
// cooldown window (safe to pull and update).
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - sourceContainer: Container whose image age to evaluate.
//   - params: Update parameters (global cooldown + LabelPrecedence).
//
// Returns:
//   - bool: true if safe to pull/update, false if deferred.
//   - error: non-nil when deferred (carries CooldownError for details).
func (c imageClient) isOutsideCooldown(
	ctx context.Context,
	sourceContainer types.Container,
	params types.UpdateParams,
) (bool, error) {
	cooldownDelay := sourceContainer.CooldownDelay(params)
	if cooldownDelay <= 0 {
		return true, nil
	}

	if sourceContainer.IsNoPull(params) || sourceContainer.IsMonitorOnly(params) {
		return true, nil
	}

	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	})

	pullOpts, err := registry.GetPullOptions(
		sourceContainer.ImageName(),
	)
	if err != nil {
		clog.WithError(err).
			Info("Failed to get pull options for cooldown check - deferring update")

		return false, &CooldownError{
			Delay: util.FormatDuration(cooldownDelay),
			err: fmt.Errorf(
				"could not get pull options for %s (cooldown: %s): %w",
				sourceContainer.ImageName(),
				util.FormatDuration(cooldownDelay),
				err,
			),
		}
	}

	creationTime, err := registry.FetchImageCreationTime(
		ctx,
		sourceContainer,
		pullOpts.RegistryAuth,
	)
	if err != nil {
		clog.WithError(err).
			Info("Image creation time unavailable - deferring update")

		return false, &CooldownError{
			Delay: util.FormatDuration(cooldownDelay),
			err: fmt.Errorf(
				"could not determine image age (cooldown: %s): %w",
				util.FormatDuration(cooldownDelay),
				err,
			),
		}
	}

	imageAge := time.Since(creationTime)

	if imageAge < 0 {
		ageStr := util.FormatDuration(imageAge)
		cooldownStr := util.FormatDuration(cooldownDelay)
		clog.WithFields(logrus.Fields{
			"image_age": ageStr,
			"cooldown":  cooldownStr,
		}).Warn("Image creation time is in the future (possible clock skew) - proceeding with update")

		return true, nil
	}

	if imageAge <= cooldownDelay {
		remaining := cooldownDelay - imageAge
		ageStr := util.FormatDuration(imageAge)
		cooldownStr := util.FormatDuration(cooldownDelay)
		remainingStr := util.FormatDuration(remaining)

		clog.WithFields(logrus.Fields{
			"image_age":   ageStr,
			"cooldown":    cooldownStr,
			"eligible_in": remainingStr,
		}).Info("Image is within cooldown period - deferring update")

		return false, &CooldownError{
			Age:       ageStr,
			Delay:     cooldownStr,
			Remaining: remainingStr,
			err:       ErrImageCooldown,
		}
	}

	ageStr := util.FormatDuration(imageAge)
	cooldownStr := util.FormatDuration(cooldownDelay)
	clog.WithFields(logrus.Fields{
		"image_age": ageStr,
		"cooldown":  cooldownStr,
	}).Info("Image age exceeds cooldown - proceeding with update")

	return true, nil
}
