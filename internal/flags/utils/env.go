package utils

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// EnvString fetches a string from an environment variable via Viper.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - string: Value or empty if unset.
func EnvString(key string) string {
	viper.MustBindEnv(key)

	return viper.GetString(key)
}

// EnvInt fetches an integer from an environment variable via Viper.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - int: Value or 0 if unset.
func EnvInt(key string) int {
	viper.MustBindEnv(key)

	return viper.GetInt(key)
}

// EnvBool fetches a boolean from an environment variable via Viper.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - bool: Value or false if unset.
func EnvBool(key string) bool {
	viper.MustBindEnv(key)

	return viper.GetBool(key)
}

// DurationFromSeconds converts a floating-point second count to time.Duration.
//
// The nanosecond product is clamped to MaxInt64/MinInt64 when it would overflow
// time.Duration's integer range.
//
// Parameters:
//   - seconds: Duration length in seconds (may be fractional).
//
// Returns:
//   - time.Duration: Clamped duration value.
func DurationFromSeconds(seconds float64) time.Duration {
	nanos := seconds * float64(time.Second)

	if nanos > float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}

	if nanos < float64(math.MinInt64) {
		return time.Duration(math.MinInt64)
	}

	return time.Duration(nanos)
}

// EnvDuration fetches a duration from an environment variable.
//
// Bare values without a time unit are treated as seconds.
//
// Parameters:
//   - key: Environment variable key.
//
// Returns:
//   - time.Duration: Value or 0 if unset.
func EnvDuration(key string) time.Duration {
	viper.MustBindEnv(key)

	if raw := os.Getenv(key); raw != "" {
		trimmed := strings.TrimSpace(raw)
		if IsPureNumeric(trimmed) {
			val, err := strconv.ParseFloat(trimmed, 64)
			if err == nil {
				return DurationFromSeconds(val)
			}
		}
	}

	return viper.GetDuration(key)
}

// IsPureNumeric reports whether str is a bare number (integer or float,
// possibly signed) with no duration unit characters.
func IsPureNumeric(str string) bool {
	if str == "" {
		return false
	}

	sawDigit := false
	sawDot := false

	for i, char := range str {
		switch {
		case char >= '0' && char <= '9':
			sawDigit = true
		case char == '.':
			if sawDot {
				return false
			}

			sawDot = true
		case char == '-' || char == '+':
			if i != 0 {
				return false
			}
		default:
			return false
		}
	}

	return sawDigit
}
