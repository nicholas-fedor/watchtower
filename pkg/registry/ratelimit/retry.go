package ratelimit

import (
	"context"
	"errors"
	"fmt"

	"github.com/cenkalti/backoff/v7"
	"github.com/rs/zerolog"
)

// Do retries operation when the registry returns a 429 that is worth retrying.
//
// Permanent errors are not retried. A Retry-After longer than maxHonorWait
// stops retries so the next Watchtower cycle can try again.
//
// Parameters:
//   - ctx: Context that bounds the retry loop.
//   - log: Logger for retry notices. May be nil.
//   - host: Registry host used for shared cooldown and quota.
//   - operation: Function to run. It should return a rate-limit Error on 429.
//
// Returns:
//   - error: The last operation error, or ctx.Err() when canceled.
func Do(ctx context.Context, log *zerolog.Logger, host string, operation func() error) error {
	_, err := DoValue(ctx, log, host, func() (struct{}, error) {
		return struct{}{}, operation()
	})

	return err
}

// DoValue is Do with a successful result.
//
// Parameters:
//   - ctx: Context that bounds the retry loop.
//   - log: Logger for retry notices. May be nil.
//   - host: Registry host used for shared cooldown and quota.
//   - operation: Function to run.
//
// Returns:
//   - T: Value from a successful attempt.
//   - error: The last operation error, or ctx.Err() when canceled.
func DoValue[T any](
	ctx context.Context,
	log *zerolog.Logger,
	host string,
	operation func() (T, error),
) (T, error) {
	var zero T

	ctxErr := ctx.Err()
	if ctxErr != nil {
		return zero, fmt.Errorf("rate-limit retry canceled: %w", ctxErr)
	}

	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = minHonorWait
	exp.MaxInterval = maxHonorWait

	result, err := backoff.Retry(ctx, func() (T, error) {
		waitErr := Wait(ctx, host)
		if waitErr != nil {
			return zero, backoff.Permanent(waitErr)
		}

		value, opErr := operation()
		if opErr == nil {
			ObserveSuccess(host)

			return value, nil
		}

		info, ok := errors.AsType[*Error](opErr)
		if !ok && !Is(opErr) {
			return zero, backoff.Permanent(opErr)
		}

		if ok {
			if info.Host == "" {
				info.Host = host
			}

			Observe(host, info)
		}

		wait, giveUp := Decision(info)
		if giveUp {
			return zero, backoff.Permanent(opErr)
		}

		if log != nil {
			log.Warn().
				Str("host", host).
				Dur("retry_after", wait).
				Msg("Registry rate limited. Retrying after delay")
		}

		return zero, backoff.RetryAfter(wait, opErr)
	},
		backoff.WithBackOff(exp),
		backoff.WithMaxTries(maxRetryTries),
		backoff.WithMaxElapsedTime(maxRetryElapsed),
	)
	if err == nil {
		return result, nil
	}

	return result, lastOperationError(err)
}

// lastOperationError unwraps a cenkalti RetryError to the last operation error.
//
// Context cancellation on the retry loop is preferred over the last 429 so
// callers can distinguish a canceled wait from a rate limit.
//
// Parameters:
//   - err: Error returned by backoff.Retry. May be a *backoff.RetryError.
//
// Returns:
//   - error: Last operation error, the context cause, or err unchanged.
func lastOperationError(err error) error {
	retryErr := backoff.AsRetryError(err)
	if retryErr == nil {
		return err
	}

	if retryErr.Cause != nil &&
		(errors.Is(retryErr.Cause, context.Canceled) || errors.Is(retryErr.Cause, context.DeadlineExceeded)) {
		return retryErr.Cause
	}

	if retryErr.LastErr != nil {
		return retryErr.LastErr
	}

	return err
}
