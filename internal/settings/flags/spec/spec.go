package spec

import (
	"errors"
	"fmt"
	"time"
)

// ListParseKind selects how multi-value env/default strings become []string.
type ListParseKind int

const (
	// ListNone is a scalar flag (not a list).
	ListNone ListParseKind = iota
	// ListCommaOrSpace splits on commas and/or spaces.
	ListCommaOrSpace
	// ListCommaOnly splits on commas only.
	ListCommaOnly
	// ListNotificationURLs uses notification URL parsing (embedded commas preserved).
	ListNotificationURLs
	// ListNative uses pflag native StringSlice/StringArray semantics only.
	ListNative
	// ListNewline splits env values on newlines only (repeatable mappings).
	ListNewline
)

// FlagKind is the pflag value kind for registration.
type FlagKind int

const (
	// KindBool is a boolean flag.
	KindBool FlagKind = iota
	// KindString is a string flag.
	KindString
	// KindInt is an int flag.
	KindInt
	// KindDuration is a time.Duration flag.
	KindDuration
	// KindStringSlice is a string slice flag.
	KindStringSlice
	// KindStringArray is a string array flag (repeatable, no CSV split by pflag).
	KindStringArray
)

// FlagSpec is the single metadata row for one CLI/env setting.
//
// Default must be the static default (not env-baked). EnvKeys lists explicit
// environment variable names (supports irregular mappings).
type FlagSpec struct {
	// Name is the long flag name (e.g. "stop-timeout").
	Name string
	// Shorthand is an optional single-letter flag.
	Shorthand string
	// Kind selects the pflag type.
	Kind FlagKind
	// Default is the static default value.
	Default any
	// Min is the smallest value an integer setting accepts. The zero value
	// rejects negative values.
	Min int
	// Max is the largest value an integer setting accepts. The zero value sets
	// no upper bound.
	Max int
	// EnvKeys are environment variable names bound to this flag.
	EnvKeys []string
	// Help is the flag help text.
	Help string
	// Deprecated is a non-empty migration hint when the flag is deprecated.
	Deprecated string
	// Hidden hides the flag from help when true.
	Hidden bool
	// ListParse selects multi-value parsing for string list kinds.
	ListParse ListParseKind
}

// Errors for setting values outside their range.
var (
	// errNegativeValue indicates a negative value for a setting that must not
	// be negative.
	errNegativeValue = errors.New("must be non-negative")
	// errBelowMinimum indicates a value below the minimum a spec declares.
	errBelowMinimum = errors.New("must be at least")
	// errAboveMaximum indicates a value above the maximum a spec declares.
	errAboveMaximum = errors.New("must be at most")
)

// CheckInt checks a value of an integer setting against the range its spec
// declares.
//
// Parameters:
//   - value: Resolved value of the setting.
//
// Returns:
//   - error: Non-nil when the value is outside the range. It names the
//     setting, the bound and the value.
func (s FlagSpec) CheckInt(value int) error {
	switch {
	case value < s.Min && s.Min == 0:
		return fmt.Errorf("%s %w, got %d", s.Name, errNegativeValue, value)
	case value < s.Min:
		return fmt.Errorf("%s %w %d, got %d", s.Name, errBelowMinimum, s.Min, value)
	case s.Max != 0 && value > s.Max:
		return fmt.Errorf("%s %w %d, got %d", s.Name, errAboveMaximum, s.Max, value)
	default:
		return nil
	}
}

// CheckDuration checks a value of a duration setting, which must not be
// negative.
//
// Parameters:
//   - value: Resolved value of the setting.
//
// Returns:
//   - error: Non-nil when the value is negative. It names the setting and the
//     value.
func (s FlagSpec) CheckDuration(value time.Duration) error {
	if value < 0 {
		return fmt.Errorf("%s %w, got %s", s.Name, errNegativeValue, value)
	}

	return nil
}
