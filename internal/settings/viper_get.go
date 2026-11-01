package settings

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/settings/flags/spec"
	"github.com/nicholas-fedor/watchtower/internal/settings/flags/utils"
)

// Errors for setting values that are out of range.
var (
	// errNegativeSetting indicates a duration or integer setting was set to a
	// negative value.
	errNegativeSetting = errors.New("must be non-negative")
	// errNonPositiveInterval indicates a poll interval of zero or less seconds.
	errNonPositiveInterval = errors.New("interval must be positive")
)

// durationValue reads a duration from Viper with bare-second env support.
//
// When the flag was not changed on the CLI and the bound env value is a pure
// number, it is treated as seconds (legacy WATCHTOWER_TIMEOUT behavior). A
// negative value from any source is an error.
//
// Parameters:
//   - vCfg: Bound Viper instance.
//   - flagSet: Parsed flag set (for Changed checks).
//   - name: Flag name.
//   - envKeys: Environment keys bound to this flag.
//
// Returns:
//   - time.Duration: Resolved duration.
//   - error: Wraps errNegativeSetting, naming the setting, when the value is negative.
func durationValue(
	vCfg *viper.Viper,
	flagSet *pflag.FlagSet,
	name string,
	envKeys []string,
) (time.Duration, error) {
	if flagSet.Changed(name) {
		d, err := flagSet.GetDuration(name)
		if err == nil {
			return checkNonNegative(name, d)
		}
	}

	envSeen := false

	for _, envKey := range envKeys {
		raw := strings.TrimSpace(os.Getenv(envKey))
		if raw == "" {
			continue
		}

		envSeen = true

		if utils.IsPureNumeric(raw) {
			d, err := bareSeconds(raw)
			if err == nil {
				return checkNonNegative(name, d)
			}

			// A bare number that does not parse, such as one beyond the float
			// range, ends the search. Like any other invalid environment value,
			// it falls back to the flag default below.
			break
		}

		d, err := time.ParseDuration(raw)
		if err == nil {
			return checkNonNegative(name, d)
		}
	}

	// Invalid env values fall back to the static pflag default, not zero.
	if envSeen {
		d, err := flagSet.GetDuration(name)
		if err == nil {
			return checkNonNegative(name, d)
		}
	}

	return checkNonNegative(name, vCfg.GetDuration(name))
}

// intValue reads an integer setting from Viper. A negative value is an error.
//
// Parameters:
//   - vCfg: Bound Viper instance.
//   - name: Flag name.
//
// Returns:
//   - int: Resolved value.
//   - error: Wraps errNegativeSetting, naming the setting, when the value is negative.
func intValue(vCfg *viper.Viper, name string) (int, error) {
	return checkNonNegative(name, vCfg.GetInt(name))
}

// checkNonNegative returns a value that is not negative unchanged. When the
// value is negative, it returns an error that names the setting and the value.
//
// Parameters:
//   - name: Setting name for the error.
//   - value: Resolved value.
//
// Returns:
//   - T: The value when it is not negative.
//   - error: Wraps errNegativeSetting when the value is negative.
func checkNonNegative[T int | time.Duration](name string, value T) (T, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s %w, got %v", name, errNegativeSetting, value)
	}

	return value, nil
}

// bareSeconds parses a pure numeric string as seconds.
//
// Overflow is clamped via utils.DurationFromSeconds. Invalid input returns an error
// so callers can fall back to the static default.
//
// Parameters:
//   - raw: Pure numeric string (no duration unit suffix).
//
// Returns:
//   - time.Duration: Parsed duration in seconds when successful.
//   - error: Non-nil when raw cannot be parsed as a float.
func bareSeconds(raw string) (time.Duration, error) {
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse bare seconds: %w", err)
	}

	return utils.DurationFromSeconds(val), nil
}

// stringSliceValue reads a string list using the FlagSpec ListParse strategy.
//
// Parameters:
//   - vCfg: Bound Viper instance.
//   - flagSet: Parsed flag set.
//   - name: Flag name.
//   - envKeys: Environment keys bound to this flag.
//   - parse: List parse strategy.
//
// Returns:
//   - []string: Resolved list (never nil).
func stringSliceValue(
	vCfg *viper.Viper,
	flagSet *pflag.FlagSet,
	name string,
	envKeys []string,
	parse spec.ListParseKind,
) []string {
	if flagSet.Changed(name) {
		switch parse {
		case spec.ListNotificationURLs, spec.ListNewline:
			vals, err := flagSet.GetStringArray(name)
			if err == nil {
				return vals
			}
		case spec.ListNone, spec.ListCommaOrSpace, spec.ListCommaOnly, spec.ListNative:
			vals, err := flagSet.GetStringSlice(name)
			if err == nil {
				return vals
			}
		}
	}

	for _, envKey := range envKeys {
		raw := os.Getenv(envKey)
		if raw == "" {
			continue
		}

		return spec.ParseList(raw, parse)
	}

	vals := vCfg.GetStringSlice(name)
	if vals == nil {
		return []string{}
	}

	return vals
}
