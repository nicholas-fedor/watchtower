package util

import (
	"math"
	"testing"
	"time"
)

// FuzzParseDuration verifies that ParseDuration never panics on arbitrary input
// and maintains its invariants: successful parses return non-negative durations,
// and failures return non-nil errors.
func FuzzParseDuration(f *testing.F) {
	// Seed corpus with representative valid inputs.
	f.Add("24h")
	f.Add("1h30m")
	f.Add("1d")
	f.Add("3d")
	f.Add("1w")
	f.Add("1M")
	f.Add("1w2d")
	f.Add("1M1w12h")
	f.Add("1.5d")
	f.Add("0")
	f.Add("")
	f.Add("90m")
	f.Add("3600s")

	// Seed corpus with representative invalid inputs.
	f.Add("abc")
	f.Add("5x")
	f.Add("1.2.3d")
	f.Add(".d")
	f.Add("1.d")
	f.Add("-1h")
	f.Add("9999999999999999999d")
	f.Add("\x00")
	f.Add("∞d")

	f.Fuzz(func(t *testing.T, input string) {
		duration, err := ParseDuration(input)
		if err != nil {
			// Error case: duration should be zero.
			if duration != 0 {
				t.Errorf("ParseDuration(%q) returned error but non-zero duration: %v", input, duration)
			}

			return
		}

		// Success case: duration should be non-negative.
		if duration < 0 {
			t.Errorf("ParseDuration(%q) returned negative duration: %v", input, duration)
		}

		// Verify that re-formatting and re-parsing produces a consistent result.
		formatted := duration.String()

		reparsed, parseErr := time.ParseDuration(formatted)
		if parseErr != nil {
			t.Errorf("duration.String() output %q could not be re-parsed: %v", formatted, parseErr)

			return
		}

		if duration != reparsed {
			t.Errorf("ParseDuration(%q) = %v, but re-parsing %q = %v", input, duration, formatted, reparsed)
		}
	})
}

// FuzzExpandDurationUnits verifies that expandDurationUnits never panics on
// arbitrary input.
func FuzzExpandDurationUnits(f *testing.F) {
	f.Add("1d")
	f.Add("3d12h")
	f.Add("1w2d")
	f.Add("1M")
	f.Add("24h")
	f.Add("1.5d")
	f.Add("0d")
	f.Add("")
	f.Add("abc")
	f.Add("1x")

	f.Fuzz(func(t *testing.T, input string) {
		// expandDurationUnits is an internal helper that transforms d/w/M to hours
		// but passes other units through unchanged. It should never panic.
		expanded, _ := expandDurationUnits(input)

		// We don't assert parseability here because expandDurationUnits doesn't
		// validate units — that's ParseDuration's responsibility.
		_ = expanded
	})
}

// FuzzMultiplyUnit verifies that multiplyUnit never panics on arbitrary
// numeric strings and produces non-scientific-notation output.
func FuzzMultiplyUnit(f *testing.F) {
	f.Add("1", 24.0)
	f.Add("0", 24.0)
	f.Add("1.5", 24.0)
	f.Add("100", 720.0)
	f.Add("0.1", 24.0)

	f.Fuzz(func(t *testing.T, numStr string, factor float64) {
		// multiplyUnit is an internal helper — it should never panic.
		result, err := multiplyUnit(numStr, factor)
		if err != nil {
			// Error case is expected for non-numeric input.
			return
		}

		// Skip scientific notation check for special float64 values
		// (NaN, +Inf, -Inf) since the output format is undefined.
		if math.IsNaN(factor) || math.IsInf(factor, 0) {
			return
		}

		// Result should not contain scientific notation (fixed format).
		for _, ch := range result {
			if ch == 'e' || ch == 'E' {
				t.Errorf("multiplyUnit(%q, %v) = %q contains scientific notation", numStr, factor, result)

				return
			}
		}
	})
}
