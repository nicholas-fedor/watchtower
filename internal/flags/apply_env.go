package flags

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/nicholas-fedor/watchtower/internal/flags/spec"
	"github.com/nicholas-fedor/watchtower/internal/flags/utils"
)

// ApplyEnvToFlags copies bound environment values onto flags that were not set on the CLI.
//
// Call after Cobra parse and before ProcessFlagAliases / SetupLogging so those
// helpers still see env-sourced values via flag Gets. Load continues to resolve
// through Viper with flag > env > default precedence. Static pflag defaults are
// never env-baked at registration time.
//
// Parameters:
//   - flagSet: Parsed persistent flag set.
//   - specs: Aggregated FlagSpec rows.
//
// Returns:
//   - error: Non-nil when a flag cannot be set from env.
func ApplyEnvToFlags(flagSet *pflag.FlagSet, specs []spec.FlagSpec) error {
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
		return fmt.Errorf("%w: %q", ErrFlagNotRegistered, flagSpec.Name)
	}

	switch flagSpec.Kind {
	case spec.KindStringSlice, spec.KindStringArray:
		parts := parseEnvList(raw, flagSpec.ListParse)

		sliceValue, ok := flag.Value.(pflag.SliceValue)
		if !ok {
			return fmt.Errorf("%w: %s is not a slice", ErrUnsupportedFlagKind, flagSpec.Name)
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

		// flagSet.Set marks Changed; clear it so env bridging stays CLI-neutral.
		err = flagSet.Set(flagSpec.Name, value)
		if err != nil {
			return fmt.Errorf("set %s: %w", flagSpec.Name, err)
		}

		flag.Changed = false

		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedFlagKind, flagSpec.Name)
	}
}

// presenceEmptyEnvKeys are env keys where presence with an empty value means true
// (https://no-color.org/). All other keys treat empty as unset.
var presenceEmptyEnvKeys = map[string]struct{}{
	"NO_COLOR": {},
}

// firstEnv returns the first usable environment value for the given keys.
//
// Empty values are skipped unless the key is in presenceEmptyEnvKeys (NO_COLOR),
// where presence alone is meaningful.
//
// Parameters:
//   - envKeys: Candidate environment variable names in priority order.
//
// Returns:
//   - string: Raw env value (may be empty only for presenceEmptyEnvKeys).
//   - bool: True when a usable entry was found.
func firstEnv(envKeys []string) (string, bool) {
	for _, key := range envKeys {
		raw, ok := os.LookupEnv(key)
		if !ok {
			continue
		}

		if raw == "" {
			if _, allowEmpty := presenceEmptyEnvKeys[key]; !allowEmpty {
				continue
			}
		}

		return raw, true
	}

	return "", false
}

// formatEnvForFlag converts a raw env string into a pflag.Set value string.
//
// For bools, a non-empty value is parsed with strconv.ParseBool. An empty raw
// value is only accepted for presenceEmptyEnvKeys (NO_COLOR) and means true;
// firstEnv must not pass empty strings for other keys.
func formatEnvForFlag(flagSpec spec.FlagSpec, raw string) (string, error) {
	switch flagSpec.Kind {
	case spec.KindBool:
		if raw == "" {
			// Presence-means-true for NO_COLOR only (see firstEnv / presenceEmptyEnvKeys).
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
		// Allow bare integers only; pflag Set accepts decimal strings.
		trimmed := strings.TrimSpace(raw)

		_, err := strconv.Atoi(trimmed)
		if err != nil {
			return "", fmt.Errorf("parse int: %w", err)
		}

		return trimmed, nil
	case spec.KindDuration:
		trimmed := strings.TrimSpace(raw)
		if utils.IsPureNumeric(trimmed) {
			val, err := strconv.ParseFloat(trimmed, 64)
			if err != nil {
				return "", fmt.Errorf("parse duration seconds: %w", err)
			}

			return time.Duration(val * float64(time.Second)).String(), nil
		}

		d, err := time.ParseDuration(trimmed)
		if err != nil {
			return "", fmt.Errorf("parse duration: %w", err)
		}

		return d.String(), nil
	case spec.KindStringSlice, spec.KindStringArray:
		return "", fmt.Errorf("%w: %s handled via Replace", ErrUnsupportedFlagKind, flagSpec.Name)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedFlagKind, flagSpec.Name)
	}
}

// parseEnvList splits a raw env list using FlagSpec ListParse.
func parseEnvList(raw string, parse spec.ListParseKind) []string {
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
