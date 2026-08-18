package ratelimit

import (
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

	time.Sleep(220 * time.Millisecond)

	got, err := DoValue(t.Context(), testLog(), "ghcr.io", func() (string, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", got)

	started := time.Now()

	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	assert.Less(t, time.Since(started), 50*time.Millisecond)
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
