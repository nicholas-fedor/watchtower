package types

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
