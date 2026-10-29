package types

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// stopTimeoutContainer is a container whose only behavior is its stop timeout.
// Calling any other method panics.
type stopTimeoutContainer struct {
	// Container leaves every other method unimplemented.
	Container

	// own is the stop timeout set on the container, or nil when it sets none.
	own *time.Duration
}

// StopTimeout returns the container's own stop timeout, and fallback when it
// sets none.
func (c stopTimeoutContainer) StopTimeout(fallback time.Duration) time.Duration {
	if c.own != nil {
		return *c.own
	}

	return fallback
}

// TestUpdateParams_DetachedTimeout verifies that detached Docker calls are
// bounded by the stop timeout plus 30 seconds. A stop timeout of zero or below
// leaves 30 seconds, and a stop timeout near the longest duration saturates
// instead of overflowing.
func TestUpdateParams_DetachedTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{name: "zero timeout", timeout: 0, want: 30 * time.Second},
		{name: "negative timeout", timeout: -time.Second, want: 30 * time.Second},
		{name: "short timeout", timeout: time.Second, want: 31 * time.Second},
		{name: "default timeout", timeout: 30 * time.Second, want: time.Minute},
		{name: "long timeout", timeout: 10 * time.Minute, want: 10*time.Minute + 30*time.Second},
		{name: "longest timeout", timeout: math.MaxInt64, want: math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, UpdateParams{Timeout: tt.timeout}.DetachedTimeout())
		})
	}
}

// TestUpdateParams_DetachedStopTimeout verifies that the bound for detached
// calls that stop a container follows the longer of the configured stop timeout
// and the container's own, plus 30 seconds.
func TestUpdateParams_DetachedStopTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		own     *time.Duration
		want    time.Duration
	}{
		{name: "no container stop timeout", timeout: 20 * time.Second, want: 50 * time.Second},
		{name: "longer container stop timeout", timeout: 20 * time.Second, own: new(2 * time.Minute), want: 150 * time.Second},
		{name: "shorter container stop timeout", timeout: 20 * time.Second, own: new(5 * time.Second), want: 50 * time.Second},
		{name: "container stop timeout without a configured one", timeout: 0, own: new(time.Minute), want: 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := UpdateParams{Timeout: tt.timeout}.DetachedStopTimeout(stopTimeoutContainer{own: tt.own})
			assert.Equal(t, tt.want, got)
		})
	}
}
