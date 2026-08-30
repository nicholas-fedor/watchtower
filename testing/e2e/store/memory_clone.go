package store

import (
	"maps"
	"slices"
	"strings"
)

// cloneRun copies CurrentIDs so callers cannot mutate store state.
//
// Parameters:
//   - run: Sitting to copy.
//
// Returns:
//   - Run: Shallow copy with cloned current IDs.
func cloneRun(run Run) Run {
	run.CurrentIDs = slices.Clone(run.CurrentIDs)

	return run
}

// cloneCase copies maps and JSON so callers cannot mutate store state.
//
// Parameters:
//   - item: Case to copy.
//
// Returns:
//   - Case: Deep copy of mutable fields.
func cloneCase(item Case) Case {
	item.Factors = maps.Clone(item.Factors)
	item.Env = maps.Clone(item.Env)
	item.Argv = slices.Clone(item.Argv)
	item.Expect = slices.Clone(item.Expect)
	item.InspectBefore = slices.Clone(item.InspectBefore)
	item.InspectAfter = slices.Clone(item.InspectAfter)
	item.Porcelain = slices.Clone(item.Porcelain)

	return item
}

// caseMatches reports whether query is a substring of the case ID or a factor.
//
// Parameters:
//   - item: Case to search.
//   - query: Substring, compared case-insensitively.
//
// Returns:
//   - bool: True when the case should be included.
func caseMatches(item Case, query string) bool {
	q := strings.ToLower(query)
	if strings.Contains(strings.ToLower(item.CaseID), q) {
		return true
	}

	for name, level := range item.Factors {
		if strings.Contains(strings.ToLower(name), q) || strings.Contains(strings.ToLower(level), q) {
			return true
		}
	}

	return false
}
