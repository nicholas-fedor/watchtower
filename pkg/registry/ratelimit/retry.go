package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/rs/zerolog"
)

// Do retries operation when the registry returns a 429 that is worth retrying.
//
// Permanent errors are not retried. A Retry-After longer than the honor window
// stops retries so the next Watchtower cycle can try again. Tiny token-bucket
// waits retry until that same window elapses.
//
// Parameters:
//   - ctx: Context that bounds the retry loop.
//   - log: Logger for retry notices. May be nil.
//   - host: Registry host used for shared cooldown and quota.
//   - operation: Function to run. It should return an [Error] on 429.
//
// Returns:
//   - error: The last operation error, or [context.Context.Err] when canceled.
func Do(ctx context.Context, log *zerolog.Logger, host string, operation func() error) error {
	_, err := DoValue(ctx, log, host, func() (struct{}, error) {
		return struct{}{}, operation()
	})

	return err
}

// DoValue is [Do] with a successful result.
//
// Parameters:
//   - ctx: Context that bounds the retry loop.
//   - log: Logger for retry notices. May be nil.
//   - host: Registry host used for shared cooldown and quota.
//   - operation: Function to run.
//
// Returns:
//   - T: Value from a successful attempt.
//   - error: The last operation error, or [context.Context.Err] when canceled.
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

	attempt := 0
	lastHonoredWait := time.Duration(0)
	lastRawRetryAfter := time.Duration(0)

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
			lastRawRetryAfter = info.RetryAfter
		}

		wait, giveUp := Decision(info)

		attempt++
		lastHonoredWait = wait

		if giveUp {
			return zero, backoff.Permanent(opErr)
		}

		wait = jitterWait(wait)

		if log != nil {
			log.Debug().
				Str("host", host).
				Dur("retry_after", wait).
				Msg("Registry rate limited. Retrying after delay")
		}

		return zero, backoff.RetryAfter(wait, opErr)
	},
		backoff.WithBackOff(exp),
		backoff.WithMaxTries(maxRetryAttempts()),
		backoff.WithMaxElapsedTime(retryElapsed),
	)
	if err == nil {
		return result, nil
	}

	err = lastOperationError(err)
	if log != nil && Is(err) {
		log.Warn().
			Err(err).
			Str("host", host).
			Int("attempts", attempt).
			Dur("honored_wait", lastHonoredWait).
			Dur("retry_after", lastRawRetryAfter).
			Msg("Registry rate limited. Stopping retries")
	}

	return result, err
}

// maxRetryAttempts is a circuit breaker derived from the elapsed budget.
//
// Token-bucket 429s retry until [retryElapsed], not a fixed handful of tries.
// This cap only stops a zero-wait loop from spinning.
//
// Returns:
//   - uint: Maximum attempts passed to [backoff.WithMaxTries].
func maxRetryAttempts() uint {
	n := retryElapsed/minHonorWait + 1
	if n < 1 {
		return 1
	}

	return uint(n)
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

// jitterWait adds nonnegative jitter so concurrent retries do not wake together.
//
// The Decision wait is the minimum. Extra delay is in
// [0, wait/equalJitterDivisor], capped so the result does not exceed
// maxHonorWait. Zero and negative waits are unchanged.
//
// Parameters:
//   - wait: Decision wait before the next attempt.
//
// Returns:
//   - time.Duration: Jittered wait, never shorter than wait when wait > 0.
func jitterWait(wait time.Duration) time.Duration {
	if wait <= 0 {
		return wait
	}

	room := maxHonorWait - wait
	if room <= 0 {
		return wait
	}

	spread := min(wait/equalJitterDivisor, room)

	//nolint:gosec // Jitter only needs to desynchronize retries, not be cryptographic.
	return wait + time.Duration(rand.Int64N(int64(spread)+1))
}
