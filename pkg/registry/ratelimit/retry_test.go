package ratelimit

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testLog returns a discarded logger for retry unit tests.
//
// Returns:
//   - *zerolog.Logger: Nop logger that writes nothing.
func testLog() *zerolog.Logger {
	logger := zerolog.Nop()

	return &logger
}

func TestJitterWaitDoesNotShortenDecisionWait(t *testing.T) {
	t.Parallel()

	wait := 100 * time.Millisecond
	seen := map[time.Duration]struct{}{}
	maxExtra := wait / equalJitterDivisor

	for range 200 {
		got := jitterWait(wait)
		assert.GreaterOrEqual(t, got, wait)
		assert.LessOrEqual(t, got, wait+maxExtra)
		seen[got] = struct{}{}
	}

	assert.Greater(t, len(seen), 1)
}

func TestJitterWaitLeavesZeroUnchanged(t *testing.T) {
	t.Parallel()

	assert.Equal(t, time.Duration(0), jitterWait(0))
}

func TestJitterWaitDoesNotExceedHonorWindow(t *testing.T) {
	t.Parallel()

	assert.Equal(t, maxHonorWait, jitterWait(maxHonorWait))
}

func TestDoLogsRetryAtDebug(t *testing.T) {
	ResetForTest()

	var buf bytes.Buffer

	log := zerolog.New(&buf)

	var attempts atomic.Int32

	err := Do(t.Context(), &log, "registry.example", func() error {
		if attempts.Add(1) < 2 {
			return &Error{RetryAfter: minHonorWait}
		}

		return nil
	})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"level":"debug"`)
	assert.Contains(t, buf.String(), "Registry rate limited. Retrying after delay")
	assert.NotContains(t, buf.String(), `"level":"warn"`)
}

func TestDoLogsExhaustionAtWarn(t *testing.T) {
	ResetForTest()

	var buf bytes.Buffer

	log := zerolog.New(&buf).Level(zerolog.DebugLevel)

	err := Do(t.Context(), &log, "registry.example", func() error {
		return &Error{RetryAfter: 2 * time.Hour}
	})
	require.ErrorIs(t, err, ErrRateLimited)
	assert.Contains(t, buf.String(), `"level":"warn"`)
	assert.Contains(t, buf.String(), "Registry rate limited. Stopping retries")
	assert.Contains(t, buf.String(), `"attempts":1`)
	assert.NotContains(t, buf.String(), "Retrying after delay")
}

func TestDoRetriesPastFiveWhenRetryAfterIsTiny(t *testing.T) {
	ResetForTest()

	var attempts atomic.Int32

	err := Do(t.Context(), testLog(), "ghcr.io", func() error {
		if attempts.Add(1) < 8 {
			return &Error{
				RetryAfter:    23722 * time.Nanosecond,
				Allowed:       44000,
				AllowedWindow: time.Minute,
				Host:          "ghcr.io",
			}
		}

		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(8), attempts.Load())
}

func TestDoGivesUpWhenTinyRetryAfterExceedsElapsedBudget(t *testing.T) {
	ResetForTest()

	retryElapsed = 450 * time.Millisecond

	defer ResetForTest()

	var buf bytes.Buffer

	log := zerolog.New(&buf).Level(zerolog.DebugLevel)

	var attempts atomic.Int32

	started := time.Now()
	err := Do(t.Context(), &log, "ghcr.io", func() error {
		attempts.Add(1)

		return &Error{
			RetryAfter:    23722 * time.Nanosecond,
			Allowed:       44000,
			AllowedWindow: time.Minute,
		}
	})
	require.ErrorIs(t, err, ErrRateLimited)
	assert.Contains(t, buf.String(), "Registry rate limited. Stopping retries")
	assert.Contains(t, buf.String(), "Retrying after delay")

	maxStep := minHonorWait + minHonorWait/equalJitterDivisor
	minAttempts := int(retryElapsed / maxStep)
	assert.GreaterOrEqual(t, attempts.Load(), int32(minAttempts))
	assert.GreaterOrEqual(t, time.Since(started), time.Duration(minAttempts-1)*minHonorWait)
}

func TestDoRetriesRateLimitedOperations(t *testing.T) {
	ResetForTest()

	var attempts atomic.Int32

	err := Do(t.Context(), testLog(), "ghcr.io", func() error {
		if attempts.Add(1) < 3 {
			return &Error{RetryAfter: minHonorWait, Allowed: 44000, AllowedWindow: time.Minute}
		}

		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestDoDoesNotRetryPermanentErrors(t *testing.T) {
	ResetForTest()

	var attempts atomic.Int32

	permanent := errors.New("manifest not found")

	err := Do(t.Context(), testLog(), "ghcr.io", func() error {
		attempts.Add(1)

		return permanent
	})
	require.ErrorIs(t, err, permanent)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestDoGivesUpWhenRetryAfterExceedsHonorWindow(t *testing.T) {
	ResetForTest()

	var attempts atomic.Int32

	err := Do(t.Context(), testLog(), "ghcr.io", func() error {
		attempts.Add(1)

		return &Error{RetryAfter: 2 * time.Hour}
	})
	require.ErrorIs(t, err, ErrRateLimited)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestDoValueReturnsSuccessfulResult(t *testing.T) {
	ResetForTest()

	got, err := DoValue(t.Context(), testLog(), "ghcr.io", func() (string, error) {
		return "digest", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "digest", got)
}

func TestDoTreatsSentinelTextWithoutWrappingAsPermanent(t *testing.T) {
	ResetForTest()

	var attempts atomic.Int32

	err := Do(t.Context(), nil, "ghcr.io", func() error {
		if attempts.Add(1) < 2 {
			return errors.New("registry rate limited: " + ErrRateLimited.Error())
		}

		return nil
	})
	// A plain string is not errors.Is(ErrRateLimited), so it must be permanent.
	require.Error(t, err)
	assert.Equal(t, int32(1), attempts.Load())
}

func TestDoRetriesWrappedErrRateLimited(t *testing.T) {
	ResetForTest()

	var attempts atomic.Int32

	err := Do(t.Context(), nil, "ghcr.io", func() error {
		if attempts.Add(1) < 2 {
			return errors.Join(ErrRateLimited)
		}

		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), attempts.Load())
}

func TestDoValueConsumesOneTokenPerAttempt(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{
		Allowed:       2,
		AllowedWindow: 200 * time.Millisecond,
	})

	got, err := DoValue(t.Context(), testLog(), "ghcr.io", func() (string, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", got)

	started := time.Now()

	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	assert.GreaterOrEqual(t, time.Since(started), 80*time.Millisecond)
}

func TestDoValueHonorsHostWaitCancellation(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{RetryAfter: 5 * time.Second})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	_, err := DoValue(ctx, testLog(), "ghcr.io", func() (string, error) {
		return "unused", nil
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestLastOperationErrorPassesThroughPlainErrors(t *testing.T) {
	t.Parallel()

	plain := errors.New("not a retry error")
	assert.Equal(t, plain, lastOperationError(plain))
	assert.NoError(t, lastOperationError(nil))
}

func TestDoStopsWhenContextCanceled(t *testing.T) {
	ResetForTest()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := Do(ctx, testLog(), "ghcr.io", func() error {
		return &Error{RetryAfter: minHonorWait}
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
