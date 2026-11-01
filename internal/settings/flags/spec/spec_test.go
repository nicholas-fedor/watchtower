package spec

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFlagSpec_CheckInt verifies the range check of an integer setting. A
// zero minimum rejects negative values. A declared minimum or maximum bounds
// the value, and a zero maximum sets no upper bound. Each error names the
// setting, the bound and the value.
func TestFlagSpec_CheckInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    FlagSpec
		value   int
		wantErr error
		wantMsg string
	}{
		{name: "zero minimum accepts zero", spec: FlagSpec{Name: "delay"}, value: 0},
		{
			name:    "zero minimum rejects a negative value",
			spec:    FlagSpec{Name: "delay"},
			value:   -1,
			wantErr: errNegativeValue,
			wantMsg: "delay must be non-negative, got -1",
		},
		{name: "declared minimum accepts the minimum", spec: FlagSpec{Name: "interval", Min: 1}, value: 1},
		{
			name:    "declared minimum rejects a lower value",
			spec:    FlagSpec{Name: "interval", Min: 1},
			value:   0,
			wantErr: errBelowMinimum,
			wantMsg: "interval must be at least 1, got 0",
		},
		{name: "declared maximum accepts the maximum", spec: FlagSpec{Name: "port", Max: 65535}, value: 65535},
		{
			name:    "declared maximum rejects a higher value",
			spec:    FlagSpec{Name: "port", Max: 65535},
			value:   65536,
			wantErr: errAboveMaximum,
			wantMsg: "port must be at most 65535, got 65536",
		},
		{name: "zero maximum sets no upper bound", spec: FlagSpec{Name: "uid"}, value: math.MaxInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.spec.CheckInt(tt.value)
			if tt.wantErr == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			assert.EqualError(t, err, tt.wantMsg)
		})
	}
}

// TestFlagSpec_CheckDuration verifies that a duration setting accepts zero
// and positive values and rejects a negative value with an error naming the
// setting and the value.
func TestFlagSpec_CheckDuration(t *testing.T) {
	t.Parallel()

	flagSpec := FlagSpec{Name: "stop-timeout", Kind: KindDuration}

	require.NoError(t, flagSpec.CheckDuration(0))
	require.NoError(t, flagSpec.CheckDuration(time.Second))

	err := flagSpec.CheckDuration(-time.Second)
	require.ErrorIs(t, err, errNegativeValue)
	assert.EqualError(t, err, "stop-timeout must be non-negative, got -1s")
}
