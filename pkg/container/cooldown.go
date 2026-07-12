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

// CooldownError represents a cooldown deferral with human-readable details
// (age, delay, remaining) and whether the window passed. It wraps
// ErrImageCooldown (or a fetch failure) so callers can use errors.Is/As
// while still extracting the fields needed for progress reports and
// rich notifications.
type CooldownError struct {
	Age        string
	Delay      string
	Remaining  string
	EligibleAt time.Time
	Passed     bool
	err        error
}

func (e *CooldownError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}

	return ErrImageCooldown.Error()
}

func (e *CooldownError) Unwrap() error { return e.err }

func (e *CooldownError) Is(target error) bool {
	if target == ErrImageCooldown {
		return e.err == nil || errors.Is(e.err, ErrImageCooldown)
	}

	return errors.Is(e.err, target)
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
	delay, skip := shouldCheckCooldown(sourceContainer, params)
	if skip {
		return true, nil
	}

	clog := logrus.WithFields(logrus.Fields{
		"container": sourceContainer.Name(),
		"image":     sourceContainer.ImageName(),
	})

	creationTime, cdErr := fetchImageCreationTime(ctx, sourceContainer, delay, clog)
	if cdErr != nil {
		return false, cdErr
	}

	return evalImageAge(creationTime, delay, clog)
}

// shouldCheckCooldown returns the effective cooldown delay and whether the
// check can be skipped (delay ≤ 0, no-pull, or monitor-only).
//
// Parameters:
//   - sourceContainer: Container to evaluate.
//   - params: Update parameters (global cooldown + LabelPrecedence).
//
// Returns:
//   - time.Duration: The effective cooldown delay (0 if skipped).
//   - bool: True if the cooldown check should be skipped entirely.
func shouldCheckCooldown(sourceContainer types.Container, params types.UpdateParams) (time.Duration, bool) {
	delay := sourceContainer.CooldownDelay(params)
	if delay <= 0 || sourceContainer.IsNoPull(params) || sourceContainer.IsMonitorOnly(params) {
		return 0, true
	}

	return delay, false
}

// fetchImageCreationTime resolves pull options and retrieves the image
// creation time from the registry. It returns a CooldownError on any failure.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts.
//   - sourceContainer: Container whose image creation time to fetch.
//   - delay: Cooldown delay for error formatting.
//   - clog: Logger entry with container/image fields.
//
// Returns:
//   - time.Time: The image creation time.
//   - error: Non-nil CooldownError on failure.
func fetchImageCreationTime(
	ctx context.Context,
	sourceContainer types.Container,
	delay time.Duration,
	clog *logrus.Entry,
) (time.Time, error) {
	pullOpts, err := registry.GetPullOptions(sourceContainer.ImageName())
	if err != nil {
		clog.WithError(err).
			Info("Failed to get pull options for cooldown check - update check unavailable")

		return time.Time{}, &CooldownError{
			Delay: util.FormatDuration(delay),
			err: fmt.Errorf(
				"could not get pull options for %s (cooldown: %s): %w",
				sourceContainer.ImageName(),
				util.FormatDuration(delay),
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
			Info("Image creation time unavailable - update check unavailable")

		return time.Time{}, &CooldownError{
			Delay: util.FormatDuration(delay),
			err: fmt.Errorf(
				"could not determine image age (cooldown: %s): %w",
				util.FormatDuration(delay),
				err,
			),
		}
	}

	return creationTime, nil
}

// evalImageAge compares image age against the cooldown delay, logging the
// result and returning (true, nil) when safe to pull or (false, CooldownError)
// when deferred.
//
// Parameters:
//   - creationTime: The image creation timestamp.
//   - delay: The cooldown delay to compare against.
//   - clog: Logger entry with container/image fields.
//
// Returns:
//   - bool: True if the image age exceeds the cooldown window.
//   - error: Non-nil CooldownError when the image is within the cooldown window.
func evalImageAge(creationTime time.Time, delay time.Duration, clog *logrus.Entry) (bool, error) {
	imageAge := time.Since(creationTime)

	if imageAge < 0 {
		logClockSkew(imageAge, delay, clog)

		return true, nil
	}

	if imageAge <= delay {
		remaining := delay - imageAge
		ageStr := util.FormatDuration(imageAge)
		cooldownStr := util.FormatDuration(delay)
		remainingStr := util.FormatDuration(remaining)
		eligibleAt := time.Now().Add(remaining)

		clog.WithFields(logrus.Fields{
			"image_age":   ageStr,
			"cooldown":    cooldownStr,
			"eligible_in": remainingStr,
			"eligible_at": eligibleAt.Format(time.RFC3339),
		}).Info("Image is within cooldown period - not eligible for update")

		return false, buildCooldownError(imageAge, delay, eligibleAt)
	}

	logCooldownExceeded(imageAge, delay, clog)

	return true, nil
}

// buildCooldownError constructs a CooldownError with age, delay, and
// remaining fields populated from the image age and cooldown delay.
//
// Parameters:
//   - imageAge: Elapsed time since the image was created.
//   - delay: The configured cooldown delay.
//   - eligibleAt: Precomputed time when the container becomes eligible.
//
// Returns:
//   - *CooldownError: Populated with formatted age, delay, and remaining strings.
func buildCooldownError(imageAge, delay time.Duration, eligibleAt time.Time) *CooldownError {
	remaining := delay - imageAge

	return &CooldownError{
		Age:        util.FormatDuration(imageAge),
		Delay:      util.FormatDuration(delay),
		Remaining:  util.FormatDuration(remaining),
		EligibleAt: eligibleAt,
		err:        ErrImageCooldown,
	}
}

// logClockSkew logs a warning when the image creation time is in the future,
// indicating possible clock skew between the host and registry.
func logClockSkew(imageAge, delay time.Duration, clog *logrus.Entry) {
	clog.WithFields(logrus.Fields{
		"image_age": util.FormatDuration(imageAge),
		"cooldown":  util.FormatDuration(delay),
	}).Warn("Image creation time is in the future (possible clock skew) - update available")
}

// logCooldownExceeded logs an info message when the image age exceeds the
// cooldown window and the pull can proceed.
func logCooldownExceeded(imageAge, delay time.Duration, clog *logrus.Entry) {
	clog.WithFields(logrus.Fields{
		"image_age": util.FormatDuration(imageAge),
		"cooldown":  util.FormatDuration(delay),
	}).Info("Image age exceeds cooldown - eligible for update")
}
