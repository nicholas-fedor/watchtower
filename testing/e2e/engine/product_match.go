package engine

import (
	"iter"
	"regexp"
	"slices"
)

// pin is one factor level that can satisfy a sitting filter.
type pin struct {
	factor int
	level  int
}

// ProductMatching streams product cases that satisfy filters from the first yield.
//
// Naive Product walks mixed-radix order, so a topic whose matching levels sit
// in the middle of Model() only appears after every later factor has cycled.
// That leaves DinD idle while the dispatcher burns CPU. ProductMatching pins
// a matching level for every filter so the first yielded case matches.
//
// Parameters:
//   - factors: Cartesian axes.
//   - filters: AND-ed regexes. Empty means the full Product.
//
// Returns:
//   - iter.Seq[Case]: Matching vectors. Falls back to Product when a filter
//     matches no factor name or level (ID-only filters).
func ProductMatching(factors []Factor, filters []*regexp.Regexp) iter.Seq[Case] {
	if len(filters) == 0 {
		return Product(factors)
	}

	groups := make([][]pin, 0, len(filters))
	for _, filter := range filters {
		group := matchingPins(factors, []*regexp.Regexp{filter})
		if len(group) == 0 {
			return Product(factors)
		}

		groups = append(groups, group)
	}

	return func(yield func(Case) bool) {
		seen := make(map[string]struct{})

		for _, combo := range pinCombos(groups) {
			for item := range productPinned(factors, combo) {
				if !matchFilters(filters, item) {
					continue
				}

				id := item.ID()
				if _, ok := seen[id]; ok {
					continue
				}

				seen[id] = struct{}{}
				if !yield(item) {
					return
				}
			}
		}
	}
}

// matchingPins lists factor levels whose name or value matches any filter.
//
// Parameters:
//   - factors: Cartesian axes.
//   - filters: Compiled regexes.
//
// Returns:
//   - []pin: Witness levels, in factor order.
func matchingPins(factors []Factor, filters []*regexp.Regexp) []pin {
	out := make([]pin, 0)

	for factorIdx, factor := range factors {
		if levelMatches(filters, factor.Name) {
			for levelIdx := range factor.Levels {
				out = append(out, pin{factor: factorIdx, level: levelIdx})
			}

			continue
		}

		for levelIdx, level := range factor.Levels {
			if levelMatches(filters, level) {
				out = append(out, pin{factor: factorIdx, level: levelIdx})
			}
		}
	}

	return out
}

// pinCombos is the cartesian product of one pin per filter, conflicts dropped.
//
// Parameters:
//   - groups: Pins that satisfy each AND-ed filter.
//
// Returns:
//   - [][]pin: Compatible pin sets.
func pinCombos(groups [][]pin) [][]pin {
	if len(groups) == 0 {
		return nil
	}

	out := [][]pin{{}}
	for _, group := range groups {
		next := make([][]pin, 0, len(out)*len(group))
		for _, prefix := range out {
			for _, extra := range group {
				combo, ok := mergePin(prefix, extra)
				if !ok {
					continue
				}

				next = append(next, combo)
			}
		}

		out = next
		if len(out) == 0 {
			return nil
		}
	}

	return out
}

// mergePin adds extra to prefix when the factor is free or already that level.
//
// Parameters:
//   - prefix: Pins already chosen.
//   - extra: Candidate pin.
//
// Returns:
//   - []pin: Merged set.
//   - bool: False on a level conflict.
func mergePin(prefix []pin, extra pin) ([]pin, bool) {
	for _, have := range prefix {
		if have.factor != extra.factor {
			continue
		}

		if have.level != extra.level {
			return nil, false
		}

		return prefix, true
	}

	return append(slices.Clone(prefix), extra), true
}

// levelMatches reports whether any filter matches s.
//
// Parameters:
//   - filters: Compiled regexes.
//   - s: Factor name or level.
//
// Returns:
//   - bool: True on match.
func levelMatches(filters []*regexp.Regexp, s string) bool {
	for _, filter := range filters {
		if filter.MatchString(s) {
			return true
		}
	}

	return false
}

// productPinned is Product with each pin's factor fixed at pin.level.
//
// Parameters:
//   - factors: Cartesian axes.
//   - pins: Factor levels to hold constant.
//
// Returns:
//   - iter.Seq[Case]: Restricted product.
func productPinned(factors []Factor, pins []pin) iter.Seq[Case] {
	pinned := slices.Clone(factors)
	for _, pin := range pins {
		factor := pinned[pin.factor]
		factor.Levels = []string{factor.Levels[pin.level]}
		pinned[pin.factor] = factor
	}

	return Product(pinned)
}
