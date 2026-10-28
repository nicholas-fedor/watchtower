package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestUpdateParams_DetachedTimeout verifies that a detached Docker call is
// bounded by the stop timeout when one is set, and by 30 seconds otherwise.
func TestUpdateParams_DetachedTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{name: "positive timeout", timeout: 2 * time.Minute, want: 2 * time.Minute},
		{name: "zero timeout", timeout: 0, want: 30 * time.Second},
		{name: "negative timeout", timeout: -time.Second, want: 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, UpdateParams{Timeout: tt.timeout}.DetachedTimeout())
		})
	}
}
