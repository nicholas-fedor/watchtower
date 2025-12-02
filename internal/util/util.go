// Package util provides utility functions for Watchtower operations.
package util

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// timeUnit represents a single unit of time (hours, minutes, or seconds) with its value and labels.
type timeUnit struct {
	value    int64  // The numeric value of the unit (e.g., 2 for 2 hours)
	singular string // The singular form of the unit (e.g., "hour")
	plural   string // The plural form of the unit (e.g., "hours")
}

// SliceEqual checks if two string slices are identical.
//
// Parameters:
//   - slice1: First slice.
//   - slice2: Second slice.
//
// Returns:
//   - bool: True if equal, false otherwise.
func SliceEqual(slice1, slice2 []string) bool {
	if len(slice1) != len(slice2) {
		return false
	}

	for i := range slice1 {
		if slice1[i] != slice2[i] {
			return false
		}
	}

	return true
}

// SliceSubtract returns elements in the first slice that are not in the second slice.
//
// Parameters:
//   - slice: Source slice.
//   - subtractFrom: Slice containing elements to exclude.
//
// Returns:
//   - []string: Slice containing elements unique to the source slice.
func SliceSubtract(slice, subtractFrom []string) []string {
	result := []string{}

	for _, element1 := range slice {
		found := slices.Contains(subtractFrom, element1)

		if !found {
			result = append(result, element1)
		}
	}

	return result
}

// MinInt returns the smaller of two integers.
//
// Parameters:
//   - a: First integer.
//   - b: Second integer.
//
// Returns:
//   - int: The smaller of the two integers.
func MinInt(a, b int) int {
	if a < b {
		return a
	}

	return b
}

// StringMapSubtract removes matching key-value pairs.
//
// Parameters:
//   - map1: Source map.
//   - map2: Map to subtract.
//
// Returns:
//   - map[string]string: New map with unique or differing entries.
func StringMapSubtract(map1, map2 map[string]string) map[string]string {
	result := map[string]string{}

	for key1, value1 := range map1 {
		if value2, ok := map2[key1]; ok {
			if value2 != value1 {
				result[key1] = value1
			}
		} else {
			result[key1] = value1
		}
	}

	return result
}

// StructMapSubtract removes matching keys.
//
// Parameters:
//   - map1: Source map.
//   - map2: Map to subtract.
//
// Returns:
//   - map[string]struct{}: New map with unique keys.
func StructMapSubtract(map1, map2 map[string]struct{}) map[string]struct{} {
	result := map[string]struct{}{}

	for key1, value1 := range map1 {
		if _, ok := map2[key1]; !ok {
			result[key1] = value1
		}
	}

	return result
}

// FormatDuration converts a time.Duration into a human-readable string representation.
//
// It breaks down the duration into hours, minutes, and seconds, formatting each unit with appropriate
// grammar (singular or plural) and returning a string like "1 hour, 2 minutes, 3 seconds" or "0 seconds"
// if the duration is zero, ensuring a user-friendly output for logs and notifications.
//
// Parameters:
//   - duration: The time.Duration to convert into a readable string.
//
// Returns:
//   - string: A formatted string representing the duration, always including at least "0 seconds".
func FormatDuration(duration time.Duration) string {
	const (
		minutesPerHour   = 60 // Number of minutes in an hour for duration breakdown
		secondsPerMinute = 60 // Number of seconds in a minute for duration breakdown
		timeUnitCount    = 3  // Number of time units (hours, minutes, seconds) for pre-allocation
	)

	// Define units with calculated values from the duration, preserving order for display.
	units := []timeUnit{
		{int64(duration.Hours()), "hour", "hours"},
		{int64(math.Mod(duration.Minutes(), minutesPerHour)), "minute", "minutes"},
		{int64(math.Mod(duration.Seconds(), secondsPerMinute)), "second", "seconds"},
	}

	parts := make([]string, 0, timeUnitCount)
	// Format each unit, forcing inclusion of seconds if no other parts exist to avoid empty output.
	for i, unit := range units {
		parts = append(
			parts,
			FormatTimeUnit(
				unit.value,
				unit.singular,
				unit.plural,
				i == len(units)-1 && len(parts) == 0,
			),
		)
	}

	// Join non-empty parts, ensuring a readable output with proper separators.
	joined := strings.Join(FilterEmpty(parts), ", ")
	if joined == "" {
		return "0 seconds" // Default output when duration is zero or all units are skipped.
	}

	return joined
}

// FormatTimeUnit formats a single time unit into a string based on its value and context.
//
// It applies singular or plural grammar, skipping leading zeros unless forced (e.g., for seconds as the last unit),
// returning an empty string for skippable zeros to maintain a concise output.
//
// Parameters:
//   - value: The numeric value of the unit (e.g., 2 for 2 hours).
//   - singular: The singular form of the unit (e.g., "hour").
//   - plural: The plural form of the unit (e.g., "hours").
//   - forceInclude: A boolean indicating whether to include the unit even if zero (e.g., for seconds as fallback).
//
// Returns:
//   - string: The formatted unit (e.g., "1 hour", "2 minutes") or empty string if skipped.
func FormatTimeUnit(value int64, singular, plural string, forceInclude bool) string {
	switch {
	case value == 1:
		return "1 " + singular
	case value > 1 || forceInclude:
		return fmt.Sprintf("%d %s", value, plural)
	default:
		return "" // Skip zero values unless forced.
	}
}

// FilterEmpty removes empty strings from a slice, returning only non-empty elements.
//
// It ensures the final formatted duration string excludes unnecessary parts, maintaining readability
// by filtering out zero-value units that were not explicitly included.
//
// Parameters:
//   - parts: A slice of strings representing formatted time units (e.g., "1 hour", "").
//
// Returns:
//   - []string: A new slice containing only the non-empty strings from the input.
func FilterEmpty(parts []string) []string {
	var filtered []string

	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}

	return filtered
}

// NormalizeContainerName trims the leading "/" from container names.
//
// Parameters:
//   - name: Container name, potentially with leading "/".
//
// Returns:
//   - string: Normalized name without leading "/".
func NormalizeContainerName(name string) string {
	return strings.TrimPrefix(name, "/")
}
