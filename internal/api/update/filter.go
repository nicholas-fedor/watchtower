package update

import (
	"regexp"
	"slices"
)

// ContainerFilter creates a filter function that matches containers by name
// pattern using Go regex syntax. Invalid regex patterns are treated as literal
// strings for exact matching. Returns a pass-through filter when patterns is
// empty.
//
// Parameters:
//   - patterns: Container name patterns to match. Supports Go regex syntax.
//
// Returns:
//   - func(string, bool) bool: Filter function that returns true when the
//     container name matches any pattern.
func ContainerFilter(patterns []string) func(string, bool) bool {
	if len(patterns) == 0 {
		return func(_ string, _ bool) bool { return true }
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	exact := make([]string, 0, len(patterns))

	for _, p := range patterns {
		re, err := regexp.Compile("^" + p + "$")
		if err == nil {
			compiled = append(compiled, re)
		} else {
			exact = append(exact, p)
		}
	}

	return func(name string, _ bool) bool {
		for _, re := range compiled {
			if re.MatchString(name) {
				return true
			}
		}

		return slices.Contains(exact, name)
	}
}
