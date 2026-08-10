package flags

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/nicholas-fedor/watchtower/internal/flags/spec"
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
// Call after Cobra has parsed flags. Static flag defaults come from Specs.
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

		// Bind all non-presence env aliases in one call so earlier EnvKeys keep precedence.
		// Presence-only keys (NO_COLOR) are applied via ApplyEnvToFlags only.
		envAliases := make([]string, 0, len(flagSpec.EnvKeys))
		for _, envKey := range flagSpec.EnvKeys {
			if IsPresenceEnvKey(envKey) {
				continue
			}

			envAliases = append(envAliases, envKey)
		}

		if len(envAliases) == 0 {
			continue
		}

		bindArgs := make([]string, 0, 1+len(envAliases))
		bindArgs = append(bindArgs, flagSpec.Name)
		bindArgs = append(bindArgs, envAliases...)

		err = vip.BindEnv(bindArgs...)
		if err != nil {
			return fmt.Errorf("bind env %s -> %v: %w", flagSpec.Name, envAliases, err)
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
