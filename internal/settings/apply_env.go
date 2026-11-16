package settings

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/nicholas-fedor/watchtower/internal/settings/flags/spec"
	"github.com/nicholas-fedor/watchtower/internal/settings/flags/utils"
)

var (
	// errFlagNotRegistered indicates a FlagSpec name was not found on the flag set.
	errFlagNotRegistered = errors.New("flag not registered")
	// errUnsupportedFlagKind indicates a FlagSpec kind is not supported.
	errUnsupportedFlagKind = errors.New("unsupported flag kind")
)

// applyEnvToFlags copies bound environment values onto flags that were not set on the CLI.
//
// It runs before the logging setup and the alias flags, so they see
// env-sourced values through the flags. The flags stay unmarked as changed, so
// the command line alone decides which settings count as explicitly set.
// Static pflag defaults are never env-baked at registration time.
//
// Parameters:
//   - flagSet: Parsed persistent flag set.
//   - specs: Settings whose environment values to apply. A spec without a
//     registered flag is skipped.
//
// Returns:
//   - error: Non-nil when a flag cannot be set from env.
func applyEnvToFlags(flagSet *pflag.FlagSet, specs []spec.FlagSpec) error {
	for _, flagSpec := range specs {
		if flagSet.Lookup(flagSpec.Name) == nil {
			continue
		}

		if flagSet.Changed(flagSpec.Name) {
			continue
		}

		raw, ok := firstEnv(flagSpec.EnvKeys)
		if !ok {
			continue
		}

		err := applyEnvValue(flagSet, flagSpec, raw)
		if err != nil {
			return fmt.Errorf("%s: %w", flagSpec.Name, err)
		}
	}

	return nil
}

// applyEnvValue sets one flag from a raw environment string without marking it Changed.
//
// Callers must only invoke this when the flag was not already Changed on the CLI.
func applyEnvValue(flagSet *pflag.FlagSet, flagSpec spec.FlagSpec, raw string) error {
	flag := flagSet.Lookup(flagSpec.Name)
	if flag == nil {
		return fmt.Errorf("%w: %q", errFlagNotRegistered, flagSpec.Name)
	}

	switch flagSpec.Kind {
	case spec.KindStringSlice, spec.KindStringArray:
		parts := spec.ParseList(raw, flagSpec.ListParse)

		sliceValue, ok := flag.Value.(pflag.SliceValue)
		if !ok {
			return fmt.Errorf("%w: %s is not a slice", errUnsupportedFlagKind, flagSpec.Name)
		}

		err := sliceValue.Replace(parts)
		if err != nil {
			return fmt.Errorf("replace %s: %w", flagSpec.Name, err)
		}

		// Replace must not count as a CLI change for *Changed consumers.
		flag.Changed = false

		return nil
	case spec.KindBool, spec.KindString, spec.KindInt, spec.KindDuration:
		value, err := formatEnvForFlag(flagSpec, raw)
		if err != nil {
			return err
		}

		// flagSet.Set marks Changed. Clear it so env bridging stays CLI-neutral.
		err = flagSet.Set(flagSpec.Name, value)
		if err != nil {
			return fmt.Errorf("set %s: %w", flagSpec.Name, err)
		}

		flag.Changed = false

		return nil
	default:
		return fmt.Errorf("%w: %s", errUnsupportedFlagKind, flagSpec.Name)
	}
}

// presenceEnvKeys are env keys where any presence enables the bound bool flag
// (https://no-color.org/). Empty, "0", and "false" still mean enabled. Other
// keys treat empty as unset and parse non-empty bools with strconv.ParseBool.
var presenceEnvKeys = map[string]struct{}{
	"NO_COLOR": {},
}

// isPresenceEnvKey reports whether envKey uses presence-means-true semantics.
func isPresenceEnvKey(envKey string) bool {
	_, ok := presenceEnvKeys[envKey]

	return ok
}

// hasPresenceEnvKey reports whether any of flagSpec's env keys is presence-based.
func hasPresenceEnvKey(flagSpec spec.FlagSpec) bool {
	return slices.ContainsFunc(flagSpec.EnvKeys, isPresenceEnvKey)
}

// firstEnv returns the first usable environment value for the given keys.
//
// Empty values are skipped unless the key is in presenceEnvKeys (NO_COLOR),
// where presence alone is meaningful (including empty, "0", and "false").
//
// Parameters:
//   - envKeys: Candidate environment variable names in priority order.
//
// Returns:
//   - string: Raw env value (may be empty only for presenceEnvKeys).
//   - bool: True when a usable entry was found.
func firstEnv(envKeys []string) (string, bool) {
	for _, key := range envKeys {
		raw, ok := os.LookupEnv(key)
		if !ok {
			continue
		}

		if raw == "" && !isPresenceEnvKey(key) {
			continue
		}

		return raw, true
	}

	return "", false
}

// formatEnvForFlag converts a raw env string into a pflag.Set value string.
//
// Presence-based bool flags (NO_COLOR) always become true when firstEnv found
// the key. Other bools parse non-empty values with strconv.ParseBool. Empty is
// not passed for those keys.
func formatEnvForFlag(flagSpec spec.FlagSpec, raw string) (string, error) {
	switch flagSpec.Kind {
	case spec.KindBool:
		// Presence means true for NO_COLOR (empty, "0", "false", "1", ...).
		if hasPresenceEnvKey(flagSpec) {
			return "true", nil
		}

		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "", fmt.Errorf("parse bool: %w", err)
		}

		return strconv.FormatBool(b), nil
	case spec.KindString:
		return raw, nil
	case spec.KindInt:
		// Environment integers are decimal. pflag parses a value with its base
		// prefix, so a leading zero would read as octal. The flag is set to
		// the decimal value instead of the raw text.
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return "", fmt.Errorf("parse int: %w", err)
		}

		return strconv.Itoa(value), nil
	case spec.KindDuration:
		trimmed := strings.TrimSpace(raw)
		if utils.IsPureNumeric(trimmed) {
			val, err := strconv.ParseFloat(trimmed, 64)
			if err != nil {
				return "", fmt.Errorf("parse duration seconds: %w", err)
			}

			// Clamp so huge bare-second values do not overflow.
			return utils.DurationFromSeconds(val).String(), nil
		}

		d, err := time.ParseDuration(trimmed)
		if err != nil {
			return "", fmt.Errorf("parse duration: %w", err)
		}

		return d.String(), nil
	case spec.KindStringSlice, spec.KindStringArray:
		return "", fmt.Errorf("%w: %s handled via Replace", errUnsupportedFlagKind, flagSpec.Name)
	default:
		return "", fmt.Errorf("%w: %s", errUnsupportedFlagKind, flagSpec.Name)
	}
}
