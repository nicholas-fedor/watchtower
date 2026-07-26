package spec

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/pflag"

	"github.com/nicholas-fedor/watchtower/internal/flags/utils"
)

// ErrUnsupportedFlagKind indicates a FlagSpec kind is not supported.
var ErrUnsupportedFlagKind = errors.New("unsupported flag kind")

// Register registers pflags from FlagSpec rows using static defaults only.
//
// Parameters:
//   - flagSet: Target flag set.
//   - specs: Domain flag specifications.
//
// Returns:
//   - error: Non-nil when registration fails.
func Register(flagSet *pflag.FlagSet, specs []FlagSpec) error {
	for _, flagSpec := range specs {
		err := registerOne(flagSet, flagSpec)
		if err != nil {
			return err
		}
	}

	return nil
}

// MustRegister registers FlagSpec rows and panics on failure.
//
// Parameters:
//   - flagSet: Target flag set.
//   - specs: Domain flag specifications.
func MustRegister(flagSet *pflag.FlagSet, specs []FlagSpec) {
	err := Register(flagSet, specs)
	if err != nil {
		panic(err)
	}
}

func registerOne(flagSet *pflag.FlagSet, flagSpec FlagSpec) error {
	switch flagSpec.Kind {
	case KindBool:
		def, _ := flagSpec.Default.(bool)
		if flagSpec.Shorthand != "" {
			flagSet.BoolP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.Bool(flagSpec.Name, def, flagSpec.Help)
		}
	case KindString:
		def, _ := flagSpec.Default.(string)
		if flagSpec.Shorthand != "" {
			flagSet.StringP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.String(flagSpec.Name, def, flagSpec.Help)
		}
	case KindInt:
		def, _ := flagSpec.Default.(int)
		if flagSpec.Shorthand != "" {
			flagSet.IntP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.Int(flagSpec.Name, def, flagSpec.Help)
		}
	case KindDuration:
		def, _ := flagSpec.Default.(time.Duration)
		if flagSpec.Shorthand != "" {
			flagSet.DurationP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.Duration(flagSpec.Name, def, flagSpec.Help)
		}
	case KindStringSlice:
		def, _ := flagSpec.Default.([]string)
		if def == nil {
			def = []string{}
		}

		if flagSpec.Shorthand != "" {
			flagSet.StringSliceP(flagSpec.Name, flagSpec.Shorthand, def, flagSpec.Help)
		} else {
			flagSet.StringSlice(flagSpec.Name, def, flagSpec.Help)
		}
	case KindStringArray:
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
