package spec

import "github.com/nicholas-fedor/watchtower/internal/flags/utils"

// ParseList splits a raw list string using the FlagSpec ListParse strategy.
//
// Parameters:
//   - raw: Raw env or default list string.
//   - parse: List parse kind from FlagSpec.
//
// Returns:
//   - []string: Parsed tokens for the given strategy.
func ParseList(raw string, parse ListParseKind) []string {
	switch parse {
	case ListCommaOnly:
		return utils.SplitCommaOnly(raw)
	case ListNotificationURLs:
		return utils.FilterEmptyStrings(utils.SplitNotificationValues(raw))
	case ListCommaOrSpace, ListNative, ListNone:
		return utils.SplitCommaOrSpace(raw)
	default:
		return utils.SplitCommaOrSpace(raw)
	}
}
