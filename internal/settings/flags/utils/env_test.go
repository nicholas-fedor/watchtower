package utils

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDurationFromSeconds verifies conversion and clamping at both ends of the
// time.Duration range. float64(math.MaxInt64) rounds up to 2^63, which int64
// cannot hold, so a product equal to it must clamp rather than convert.
func TestDurationFromSeconds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		seconds float64
		want    time.Duration
	}{
		{name: "zero", seconds: 0, want: 0},
		{name: "fractional", seconds: 1.5, want: 1500 * time.Millisecond},
		{name: "negative", seconds: -2, want: -2 * time.Second},
		{
			name:    "product equals 2^63",
			seconds: float64(math.MaxInt64) / float64(time.Second),
			want:    time.Duration(math.MaxInt64),
		},
		{name: "above range", seconds: 1e10, want: time.Duration(math.MaxInt64)},
		{
			name:    "product equals -2^63",
			seconds: float64(math.MinInt64) / float64(time.Second),
			want:    time.Duration(math.MinInt64),
		},
		{name: "below range", seconds: -1e10, want: time.Duration(math.MinInt64)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, DurationFromSeconds(tc.seconds))
		})
	}
}

// TestIsPureNumeric verifies IsPureNumeric behavior with table-driven cases.
func TestIsPureNumeric(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid cases
		{name: "zero", input: "0", want: true},
		{name: "positive integer", input: "42", want: true},
		{name: "large integer (legacy common case)", input: "300", want: true},
		{name: "simple float", input: "1.5", want: true},
		{name: "leading dot", input: ".5", want: true},
		{name: "trailing dot", input: "1.", want: true},
		{name: "negative integer", input: "-10", want: true},
		{name: "explicit positive", input: "+3", want: true},
		{name: "negative float", input: "-1.5", want: true},
		{name: "positive leading dot", input: "+.5", want: true},
		{name: "negative leading dot", input: "-.5", want: true},

		// Invalid: no digits
		{name: "empty string", input: "", want: false},
		{name: "just decimal point", input: ".", want: false},
		{name: "just plus sign", input: "+", want: false},
		{name: "just minus sign", input: "-", want: false},
		{name: "plus and dot only", input: "+.", want: false},
		{name: "minus and dot only", input: "-.", want: false},

		// Invalid: multiple dots
		{name: "multiple dots", input: "1.2.3", want: false},
		{name: "double dot", input: "1..2", want: false},
		{name: "three dots", input: "1.2.3.4", want: false},

		// Invalid: misplaced or multiple signs
		{name: "minus after digit", input: "1-2", want: false},
		{name: "plus at end", input: "12+", want: false},
		{name: "plus in middle", input: "1+2", want: false},
		{name: "trailing minus", input: "5-", want: false},
		{name: "multiple signs at start", input: "+-1", want: false},
		{name: "mixed signs", input: "-+5", want: false},

		// Invalid: other characters
		{name: "contains letter", input: "1a", want: false},
		{name: "scientific notation", input: "1e3", want: false},
		{name: "thousands separator", input: "1,000", want: false},
		{name: "duration unit seconds", input: "30s", want: false},
		{name: "duration unit minutes", input: "2m", want: false},
		{name: "embedded space", input: "1 2", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsPureNumeric(tc.input)
			assert.Equal(t, tc.want, got, "IsPureNumeric(%q)", tc.input)
		})
	}
}
