package units

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatDuration_Zero verifies that FormatDuration returns "0 seconds" for zero duration.
func TestFormatDuration_Zero(t *testing.T) {
	t.Parallel()

	result := FormatDuration(0)
	assert.Equal(t, "0 seconds", result)
}

// TestFormatDuration_SecondsOnly verifies that FormatDuration formats seconds correctly.
func TestFormatDuration_SecondsOnly(t *testing.T) {
	t.Parallel()

	result := FormatDuration(45 * time.Second)
	assert.Equal(t, "45 seconds", result)
}

// TestFormatDuration_MinutesAndSeconds verifies that FormatDuration formats minutes and seconds correctly.
func TestFormatDuration_MinutesAndSeconds(t *testing.T) {
	t.Parallel()

	result := FormatDuration(2*time.Minute + 30*time.Second)
	assert.Equal(t, "2 minutes 30 seconds", result)
}

// TestFormatDuration_HoursMinutesSeconds verifies that FormatDuration formats hours, minutes, and seconds correctly.
func TestFormatDuration_HoursMinutesSeconds(t *testing.T) {
	t.Parallel()

	result := FormatDuration(1*time.Hour + 15*time.Minute + 45*time.Second)
	assert.Equal(t, "1 hour 15 minutes 45 seconds", result)
}

// TestFormatDuration_SingleValues verifies that FormatDuration uses singular forms for single units.
func TestFormatDuration_SingleValues(t *testing.T) {
	t.Parallel()

	result := FormatDuration(1*time.Hour + 1*time.Minute + 1*time.Second)
	assert.Equal(t, "1 hour 1 minute 1 second", result)
}

// TestFormatDuration_LargeDuration verifies that FormatDuration handles large durations correctly.
func TestFormatDuration_LargeDuration(t *testing.T) {
	t.Parallel()

	result := FormatDuration(25*time.Hour + 30*time.Minute)
	assert.Equal(t, "1 day 1 hour 30 minutes", result)
}

// TestFormatDuration_Days verifies that FormatDuration breaks down into days.
func TestFormatDuration_Days(t *testing.T) {
	t.Parallel()

	result := FormatDuration(72 * time.Hour)
	assert.Equal(t, "3 days", result)
}

// TestFormatDuration_Weeks verifies that FormatDuration breaks down into weeks.
func TestFormatDuration_Weeks(t *testing.T) {
	t.Parallel()

	result := FormatDuration(168 * time.Hour)
	assert.Equal(t, "1 week", result)
}

// TestFormatDuration_WeeksAndDays verifies combined weeks and days.
func TestFormatDuration_WeeksAndDays(t *testing.T) {
	t.Parallel()

	result := FormatDuration(216 * time.Hour) // 168 + 48 = 9 days
	assert.Equal(t, "1 week 2 days", result)
}

// TestFormatDuration_AllUnits verifies all five units combined.
func TestFormatDuration_AllUnits(t *testing.T) {
	t.Parallel()

	result := FormatDuration(8*24*time.Hour + 3*time.Hour + 15*time.Minute + 30*time.Second)
	assert.Equal(t, "1 week 1 day 3 hours 15 minutes 30 seconds", result)
}

// TestFormatTimeUnit_SingleValues verifies that FormatTimeUnit uses singular forms for single units.
func TestFormatTimeUnit_SingleValues(t *testing.T) {
	t.Parallel()

	result := formatTimeUnit(1, "hour", "hours", false)
	assert.Equal(t, "1 hour", result)

	result = formatTimeUnit(1, "minute", "minutes", false)
	assert.Equal(t, "1 minute", result)

	result = formatTimeUnit(1, "second", "seconds", false)
	assert.Equal(t, "1 second", result)
}

// TestFormatTimeUnit_PluralValues verifies that FormatTimeUnit uses plural forms for multiple units.
func TestFormatTimeUnit_PluralValues(t *testing.T) {
	t.Parallel()

	result := formatTimeUnit(2, "hour", "hours", false)
	assert.Equal(t, "2 hours", result)

	result = formatTimeUnit(5, "minute", "minutes", false)
	assert.Equal(t, "5 minutes", result)
}

// TestFormatTimeUnit_ZeroNotForced verifies that FormatTimeUnit returns empty string for zero values when not forced.
func TestFormatTimeUnit_ZeroNotForced(t *testing.T) {
	t.Parallel()

	result := formatTimeUnit(0, "hour", "hours", false)
	assert.Empty(t, result)
}

// TestFormatTimeUnit_ZeroForced verifies that FormatTimeUnit returns formatted string for zero values when forced.
func TestFormatTimeUnit_ZeroForced(t *testing.T) {
	t.Parallel()

	result := formatTimeUnit(0, "second", "seconds", true)
	assert.Equal(t, "0 seconds", result)
}

// TestFilterEmpty_Mixed verifies that FilterEmpty removes empty strings and keeps non-empty ones.
func TestFilterEmpty_Mixed(t *testing.T) {
	t.Parallel()

	input := []string{"1 hour", "", "30 minutes", "", "45 seconds"}
	result := filterEmpty(input)
	assert.Equal(t, []string{"1 hour", "30 minutes", "45 seconds"}, result)
}

// TestFilterEmpty_AllEmpty verifies that FilterEmpty returns empty slice when all inputs are empty.
func TestFilterEmpty_AllEmpty(t *testing.T) {
	t.Parallel()

	input := []string{"", "", ""}
	result := filterEmpty(input)
	assert.Equal(t, []string(nil), result)
}

// TestFilterEmpty_NoEmpty verifies that FilterEmpty returns all elements when none are empty.
func TestFilterEmpty_NoEmpty(t *testing.T) {
	t.Parallel()

	input := []string{"1 hour", "30 minutes", "45 seconds"}
	result := filterEmpty(input)
	assert.Equal(t, []string{"1 hour", "30 minutes", "45 seconds"}, result)
}

// TestFilterEmpty_EmptyInput verifies that FilterEmpty returns empty slice for empty input.
func TestFilterEmpty_EmptyInput(t *testing.T) {
	t.Parallel()

	input := []string{}
	result := filterEmpty(input)
	assert.Equal(t, []string(nil), result)
}

// TestParseDuration verifies the extended duration parser handles standard and extended units.
func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "empty string", input: "", want: 0},
		{name: "zero string", input: "0", want: 0},
		{name: "standard hours", input: "24h", want: 24 * time.Hour},
		{name: "standard minutes", input: "30m", want: 30 * time.Minute},
		{name: "standard combined", input: "1h30m", want: time.Hour + 30*time.Minute},
		{name: "one day", input: "1d", want: 24 * time.Hour},
		{name: "three days", input: "3d", want: 72 * time.Hour},
		{name: "one week", input: "1w", want: 7 * 24 * time.Hour},
		{name: "one month", input: "1M", want: 30 * 24 * time.Hour},
		{name: "week and days", input: "1w2d", want: 9 * 24 * time.Hour},
		{name: "month and week", input: "1M1w", want: 37 * 24 * time.Hour},
		{name: "mixed units", input: "1M2w3d12h", want: (30+14+3)*24*time.Hour + 12*time.Hour},
		{name: "decimal days", input: "1.5d", want: 36 * time.Hour},
		{name: "invalid input", input: "abc", wantErr: true},
		{name: "invalid unit", input: "5x", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseDuration(tc.input)
			if tc.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			duration: 0,
			expected: "0 seconds",
		},
		{
			name:     "only seconds",
			duration: 45 * time.Second,
			expected: "45 seconds",
		},
		{
			name:     "minutes and seconds",
			duration: 2*time.Minute + 30*time.Second,
			expected: "2 minutes 30 seconds",
		},
		{
			name:     "hours, minutes, seconds",
			duration: 1*time.Hour + 30*time.Minute + 45*time.Second,
			expected: "1 hour 30 minutes 45 seconds",
		},
		{
			name:     "single hour",
			duration: 1 * time.Hour,
			expected: "1 hour",
		},
		{
			name:     "single minute",
			duration: 1 * time.Minute,
			expected: "1 minute",
		},
		{
			name:     "single second",
			duration: 1 * time.Second,
			expected: "1 second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTimeUnit(t *testing.T) {
	tests := []struct {
		name         string
		value        int64
		singular     string
		plural       string
		forceInclude bool
		expected     string
	}{
		{
			name:         "zero value not forced",
			value:        0,
			singular:     "second",
			plural:       "seconds",
			forceInclude: false,
			expected:     "",
		},
		{
			name:         "zero value forced",
			value:        0,
			singular:     "second",
			plural:       "seconds",
			forceInclude: true,
			expected:     "0 seconds",
		},
		{
			name:         "singular value",
			value:        1,
			singular:     "hour",
			plural:       "hours",
			forceInclude: false,
			expected:     "1 hour",
		},
		{
			name:         "plural value",
			value:        5,
			singular:     "minute",
			plural:       "minutes",
			forceInclude: false,
			expected:     "5 minutes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTimeUnit(tt.value, tt.singular, tt.plural, tt.forceInclude)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilterEmpty(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "all empty",
			input:    []string{"", "", ""},
			expected: nil,
		},
		{
			name:     "all non-empty",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "mixed empty and non-empty",
			input:    []string{"", "a", "", "b", ""},
			expected: []string{"a", "b"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterEmpty(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
