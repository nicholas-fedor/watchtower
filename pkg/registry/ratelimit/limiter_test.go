package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitRespectsHostCooldown(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{RetryAfter: 80 * time.Millisecond})

	started := time.Now()
	err := Wait(t.Context(), "ghcr.io")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(started), 70*time.Millisecond)
}

func TestWaitDoesNotBlockOtherHosts(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{RetryAfter: time.Hour})

	started := time.Now()
	err := Wait(t.Context(), "index.docker.io")
	require.NoError(t, err)
	assert.Less(t, time.Since(started), 50*time.Millisecond)
}

func TestWaitHonorsAllowedBudget(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{
		Allowed:       2,
		AllowedWindow: 200 * time.Millisecond,
	})

	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	require.NoError(t, Wait(t.Context(), "ghcr.io"))

	started := time.Now()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	err := Wait(ctx, "ghcr.io")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(started), 80*time.Millisecond)
}

func TestWaitCooldownRespectsHostCooldown(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{RetryAfter: 80 * time.Millisecond})

	started := time.Now()
	err := WaitCooldown(t.Context(), "ghcr.io")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(started), 70*time.Millisecond)
}

func TestWaitCooldownDoesNotConsumeQuotaToken(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{
		Allowed:       2,
		AllowedWindow: 200 * time.Millisecond,
	})

	time.Sleep(220 * time.Millisecond)
	require.NoError(t, WaitCooldown(t.Context(), "ghcr.io"))

	started := time.Now()

	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	assert.Less(t, time.Since(started), 50*time.Millisecond)
}

func TestWaitCooldownEmptyHostReturnsImmediately(t *testing.T) {
	ResetForTest()

	started := time.Now()
	err := WaitCooldown(t.Context(), "")
	require.NoError(t, err)
	assert.Less(t, time.Since(started), 20*time.Millisecond)
}

func TestWaitCooldownCancelsWithContext(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{RetryAfter: 5 * time.Second})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	err := WaitCooldown(ctx, "ghcr.io")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWaitEmptyHostReturnsImmediately(t *testing.T) {
	ResetForTest()

	started := time.Now()
	err := Wait(t.Context(), "")
	require.NoError(t, err)
	assert.Less(t, time.Since(started), 20*time.Millisecond)
}

func TestObserveIgnoresEmptyHostAndNilInfo(t *testing.T) {
	ResetForTest()

	Observe("", &Error{RetryAfter: time.Hour})
	Observe("ghcr.io", nil)

	started := time.Now()

	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	assert.Less(t, time.Since(started), 20*time.Millisecond)
}

func TestObserveDoesNotShrinkExistingCooldown(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{RetryAfter: 80 * time.Millisecond})
	Observe("ghcr.io", &Error{RetryAfter: time.Millisecond})

	started := time.Now()

	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	assert.GreaterOrEqual(t, time.Since(started), 70*time.Millisecond)
}

func TestObserveSuccessIgnoresUnknownHost(t *testing.T) {
	ResetForTest()

	ObserveSuccess("")
	ObserveSuccess("ghcr.io")

	require.NoError(t, Wait(t.Context(), "ghcr.io"))
}

func TestObserveSuccessDoesNotConsumeReservedToken(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{
		Allowed:       2,
		AllowedWindow: 200 * time.Millisecond,
	})

	time.Sleep(220 * time.Millisecond)
	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	ObserveSuccess("ghcr.io")

	started := time.Now()

	require.NoError(t, Wait(t.Context(), "ghcr.io"))
	assert.Less(t, time.Since(started), 50*time.Millisecond)
}

func TestRefillLocked(t *testing.T) {
	t.Parallel()

	t.Run("missing budget is a no-op", func(t *testing.T) {
		t.Parallel()

		state := &hostState{}
		state.refillLocked(time.Now())
		assert.InDelta(t, 0.0, state.tokens, 0.001)
	})

	t.Run("zero last refill starts at full budget", func(t *testing.T) {
		t.Parallel()

		state := &hostState{allowed: 5, window: time.Second}
		state.refillLocked(time.Now())
		assert.InDelta(t, 5.0, state.tokens, 0.001)
	})

	t.Run("future last refill does not change tokens", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		state := &hostState{
			allowed:    5,
			window:     time.Second,
			tokens:     2,
			lastRefill: now.Add(time.Second),
		}
		state.refillLocked(now)
		assert.InDelta(t, 2.0, state.tokens, 0.001)
	})

	t.Run("caps tokens at the advertised budget", func(t *testing.T) {
		t.Parallel()

		state := &hostState{
			allowed:    10,
			window:     time.Second,
			tokens:     9,
			lastRefill: time.Now().Add(-time.Hour),
		}
		state.refillLocked(time.Now())
		assert.InDelta(t, 10.0, state.tokens, 0.001)
	})
}

func TestWaitCancelsWithContext(t *testing.T) {
	ResetForTest()

	Observe("ghcr.io", &Error{RetryAfter: 5 * time.Second})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	err := Wait(ctx, "ghcr.io")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
