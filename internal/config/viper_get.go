package config

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/flags/spec"
	"github.com/nicholas-fedor/watchtower/internal/flags/utils"
)

// durationValue reads a duration from Viper with bare-second env support.
//
// When the flag was not changed on the CLI and the bound env value is a pure
// number, it is treated as seconds (legacy WATCHTOWER_TIMEOUT behavior).
//
// Parameters:
//   - v: Bound Viper instance.
//   - flagSet: Parsed flag set (for Changed checks).
//   - name: Flag name.
//   - envKeys: Environment keys bound to this flag.
//
// Returns:
//   - time.Duration: Resolved duration.
func durationValue(
	vip *viper.Viper,
	flagSet *pflag.FlagSet,
	name string,
	envKeys []string,
) time.Duration {
	if flagSet.Changed(name) {
		d, err := flagSet.GetDuration(name)
		if err == nil {
			return d
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
			return bareSeconds(raw)
		}

		d, err := time.ParseDuration(raw)
		if err == nil {
			return d
		}
	}

	// Invalid env values fall back to the static pflag default, not zero.
	if envSeen {
		d, err := flagSet.GetDuration(name)
		if err == nil {
			return d
		}
	}

	return vip.GetDuration(name)
}

// bareSeconds parses a pure numeric string as seconds.
func bareSeconds(raw string) time.Duration {
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}

	nanos := val * float64(time.Second)

	if nanos > float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}

	if nanos < float64(math.MinInt64) {
		return time.Duration(math.MinInt64)
	}

	return time.Duration(nanos)
}

// stringSliceValue reads a string list using the FlagSpec ListParse strategy.
//
// Parameters:
//   - v: Bound Viper instance.
//   - flagSet: Parsed flag set.
//   - name: Flag name.
//   - envKeys: Environment keys bound to this flag.
//   - parse: List parse strategy.
//
// Returns:
//   - []string: Resolved list (never nil).
func stringSliceValue(
	vip *viper.Viper,
	flagSet *pflag.FlagSet,
	name string,
	envKeys []string,
	parse spec.ListParseKind,
) []string {
	if flagSet.Changed(name) {
		switch parse {
		case spec.ListNotificationURLs:
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

		return parseList(raw, parse)
	}

	vals := vip.GetStringSlice(name)
	if vals == nil {
		return []string{}
	}

	return vals
}

// parseList applies a ListParseKind to a raw env/default string.
func parseList(raw string, parse spec.ListParseKind) []string {
	switch parse {
	case spec.ListCommaOnly:
		return utils.SplitCommaOnly(raw)
	case spec.ListNotificationURLs:
		return utils.FilterEmptyStrings(utils.SplitNotificationValues(raw))
	case spec.ListCommaOrSpace, spec.ListNative, spec.ListNone:
		return utils.SplitCommaOrSpace(raw)
	default:
		return utils.SplitCommaOrSpace(raw)
	}
}
