package units

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// FuzzParseDuration verifies that ParseDuration never panics and returns
// either a valid duration or an error for any input string.
func FuzzParseDuration(f *testing.F) {
	f.Add("30s")
	f.Add("1m")
	f.Add("1h")
	f.Add("1d")
	f.Add("1w")
	f.Add("1M")
	f.Add("1w2d")
	f.Add("2M3w")
	f.Add("1M15d12h")
	f.Add("")
	f.Add("0")
	f.Add("-1s")
	f.Add("abc")
	f.Add("99999999999999999999999h")

	f.Fuzz(func(t *testing.T, s string) {
		d, err := ParseDuration(s)
		if err != nil {
			assert.Equal(t, d, time.Duration(0), "error should return zero duration")

			return
		}

		assert.GreaterOrEqual(t, d, time.Duration(0), "parsed duration should be non-negative: %v", d)

		if s == "" || s == "0" {
			assert.Equal(t, time.Duration(0), d, "empty or zero should return zero duration")
		}
	})
}

// FuzzFilterEmpty verifies that FilterEmpty correctly removes empty strings.
// Uses bytes-based fuzzing to avoid []string limitation.
func FuzzFilterEmpty(f *testing.F) {
	f.Add([]byte("a,b,c"))
	f.Add([]byte(",,,"))
	f.Add([]byte(",a,,b,"))
	f.Add([]byte(""))

	f.Fuzz(func(t *testing.T, data []byte) {
		parts := strings.Split(string(data), ",")
		result := filterEmpty(parts)

		for _, part := range result {
			assert.NotEmpty(t, part, "result should not contain empty strings")
		}
	})
}
