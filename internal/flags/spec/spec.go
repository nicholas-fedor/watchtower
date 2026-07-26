// Package spec defines shared flag metadata used by domain registration and Viper bind.
//
// Domain packages depend on this package only (no import of parent flags), avoiding cycles.
package spec

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
