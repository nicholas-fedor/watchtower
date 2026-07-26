package flags

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/flags/spec"
	"github.com/nicholas-fedor/watchtower/internal/flags/utils"
)

var (
	// ErrFlagNotRegistered indicates a FlagSpec name was not found on the flag set.
	ErrFlagNotRegistered = errors.New("flag not registered")
	// ErrInvalidFlagDefault indicates a FlagSpec default value has the wrong type.
	ErrInvalidFlagDefault = errors.New("invalid flag default")
	// ErrUnsupportedFlagKind indicates a FlagSpec kind is not supported.
	ErrUnsupportedFlagKind = errors.New("unsupported flag kind")
)

// BindAll applies Viper defaults, flag binds, and env binds from FlagSpec rows.
//
// Call after Cobra has parsed flags. Static flag defaults come from Specs;
// env values participate through BindEnv without baking into pflag defaults.
//
// Parameters:
//   - vip: Local Viper instance for this process load.
//   - flagSet: Parsed persistent flag set.
//   - specs: Aggregated domain flag specifications.
//
// Returns:
//   - error: Non-nil when a bind or default application fails.
func BindAll(vip *viper.Viper, flagSet *pflag.FlagSet, specs []spec.FlagSpec) error {
	for _, flagSpec := range specs {
		err := applyDefault(vip, flagSpec)
		if err != nil {
			return fmt.Errorf("default %s: %w", flagSpec.Name, err)
		}

		flag := flagSet.Lookup(flagSpec.Name)
		if flag == nil {
			return fmt.Errorf("%w: %q", ErrFlagNotRegistered, flagSpec.Name)
		}

		err = vip.BindPFlag(flagSpec.Name, flag)
		if err != nil {
			return fmt.Errorf("bind flag %s: %w", flagSpec.Name, err)
		}

		for _, envKey := range flagSpec.EnvKeys {
			// Presence-only keys (NO_COLOR) are applied via ApplyEnvToFlags and must
			// not BindEnv, or Viper would re-parse values like "0"/"false" as false.
			if IsPresenceEnvKey(envKey) {
				continue
			}

			err = vip.BindEnv(flagSpec.Name, envKey)
			if err != nil {
				return fmt.Errorf("bind env %s -> %s: %w", flagSpec.Name, envKey, err)
			}
		}
	}

	return nil
}

// applyDefault sets the Viper default for a FlagSpec.
//
// Parameters:
//   - vip: Viper instance.
//   - flagSpec: Flag specification.
//
// Returns:
//   - error: Non-nil when the default type is unsupported.
func applyDefault(vip *viper.Viper, flagSpec spec.FlagSpec) error {
	switch flagSpec.Kind {
	case spec.KindBool:
		b, ok := flagSpec.Default.(bool)
		if !ok && flagSpec.Default != nil {
			return fmt.Errorf("%w: bool %s", ErrInvalidFlagDefault, flagSpec.Name)
		}

		vip.SetDefault(flagSpec.Name, b)
	case spec.KindString:
		str, _ := flagSpec.Default.(string)
		vip.SetDefault(flagSpec.Name, str)
	case spec.KindInt:
		n, ok := flagSpec.Default.(int)
		if !ok && flagSpec.Default != nil {
			return fmt.Errorf("%w: int %s", ErrInvalidFlagDefault, flagSpec.Name)
		}

		vip.SetDefault(flagSpec.Name, n)
	case spec.KindDuration:
		d, ok := flagSpec.Default.(time.Duration)
		if !ok && flagSpec.Default != nil {
			return fmt.Errorf("%w: duration %s", ErrInvalidFlagDefault, flagSpec.Name)
		}

		vip.SetDefault(flagSpec.Name, d)
	case spec.KindStringSlice, spec.KindStringArray:
		switch typed := flagSpec.Default.(type) {
		case []string:
			vip.SetDefault(flagSpec.Name, typed)
		case nil:
			vip.SetDefault(flagSpec.Name, []string{})
		default:
			return fmt.Errorf("%w: string slice %s", ErrInvalidFlagDefault, flagSpec.Name)
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedFlagKind, flagSpec.Name)
	}

	return nil
}

// RegisterFromSpecs registers pflags from FlagSpec rows using static defaults only.
//
// Parameters:
//   - flagSet: Target flag set.
//   - specs: Domain flag specifications.
//
// Returns:
//   - error: Non-nil when registration fails.
func RegisterFromSpecs(flagSet *pflag.FlagSet, specs []spec.FlagSpec) error {
	for _, flagSpec := range specs {
		err := registerOne(flagSet, flagSpec)
		if err != nil {
			return err
		}
	}

	return nil
}

// registerOne registers a single FlagSpec onto flagSet.
//
// Parameters:
//   - flagSet: Target flag set.
//   - flagSpec: Flag specification.
//
// Returns:
//   - error: Non-nil when the kind/default is invalid.
func registerOne(flagSet *pflag.FlagSet, flagSpec spec.FlagSpec) error {
	switch flagSpec.Kind {
	case spec.KindBool:
		def, _ := flagSpec.Default.(bool)
		if flagSpec.Shorthand != "" {
			flagSet.BoolP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.Bool(flagSpec.Name, def, flagSpec.Help)
		}
	case spec.KindString:
		def, _ := flagSpec.Default.(string)
		if flagSpec.Shorthand != "" {
			flagSet.StringP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.String(flagSpec.Name, def, flagSpec.Help)
		}
	case spec.KindInt:
		def, _ := flagSpec.Default.(int)
		if flagSpec.Shorthand != "" {
			flagSet.IntP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.Int(flagSpec.Name, def, flagSpec.Help)
		}
	case spec.KindDuration:
		def, _ := flagSpec.Default.(time.Duration)
		if flagSpec.Shorthand != "" {
			flagSet.DurationP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.Duration(flagSpec.Name, def, flagSpec.Help)
		}
	case spec.KindStringSlice:
		def, _ := flagSpec.Default.([]string)
		if def == nil {
			def = []string{}
		}

		if flagSpec.Shorthand != "" {
			flagSet.StringSliceP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.StringSlice(flagSpec.Name, def, flagSpec.Help)
		}
	case spec.KindStringArray:
		def, _ := flagSpec.Default.([]string)
		if def == nil {
			def = []string{}
		}

		if flagSpec.Shorthand != "" {
			flagSet.StringArrayP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.StringArray(flagSpec.Name, def, flagSpec.Help)
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedFlagKind, flagSpec.Name)
	}

	if flagSpec.Deprecated != "" {
		utils.MarkFlagDeprecated(flagSet, flagSpec.Name, flagSpec.Deprecated)
	}

	if flagSpec.Hidden {
		err := flagSet.MarkHidden(flagSpec.Name)
		if err != nil {
			return fmt.Errorf("hide %s: %w", flagSpec.Name, err)
		}
	}

	return nil
}

// CollectSpecs aggregates FlagSpec rows from domain Specs functions.
//
// Parameters:
//   - groups: Domain Specs() results.
//
// Returns:
//   - []spec.FlagSpec: Combined specification list.
func CollectSpecs(groups ...[]spec.FlagSpec) []spec.FlagSpec {
	var all []spec.FlagSpec

	for _, group := range groups {
		all = append(all, group...)
	}

	return all
}
