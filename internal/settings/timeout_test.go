package settings

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStopTimeout_LegacyBareNumberAsSeconds verifies bare numeric WATCHTOWER_TIMEOUT
// values are interpreted as seconds at Load time, and that a value that does
// not parse stops Load with an error naming the environment step.
func TestStopTimeout_LegacyBareNumberAsSeconds(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
		wantErr  bool
	}{
		{name: "default (no env) is 30s", envValue: "", expected: 30 * time.Second},
		{name: "bare integer as seconds", envValue: "60", expected: 60 * time.Second},
		{name: "larger bare integer", envValue: "300", expected: 300 * time.Second},
		{name: "bare float seconds", envValue: "1.5", expected: 1500 * time.Millisecond},
		{name: "with explicit unit s", envValue: "45s", expected: 45 * time.Second},
		{name: "with unit m", envValue: "2m", expected: 2 * time.Minute},
		{name: "zero value", envValue: "0", expected: 0},
		{name: "invalid non-numeric is rejected", envValue: "abc", wantErr: true},
		{name: "invalid multi-decimal is rejected", envValue: "12.34.56", wantErr: true},
		{name: "positive sign prefix", envValue: "+10", expected: 10 * time.Second},
		{name: "whitespace trimmed", envValue: "  30  ", expected: 30 * time.Second},
		{
			name:     "unparseable huge integer is rejected",
			envValue: "1" + strings.Repeat("0", 1000),
			wantErr:  true,
		},
		{
			name:     "overflow clamps to max int64",
			envValue: "9223372038",
			expected: time.Duration(math.MaxInt64),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envValue != "" {
				t.Setenv("WATCHTOWER_TIMEOUT", tc.envValue)
			} else {
				_ = os.Unsetenv("WATCHTOWER_TIMEOUT")
			}

			cfg, _, err := Load(newTestLogger(t), newParsedCommand(t), nil)
			if tc.wantErr {
				require.ErrorContains(t, err, "apply environment configuration: stop-timeout:")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expected, cfg.Update.StopTimeout)
		})
	}
}
