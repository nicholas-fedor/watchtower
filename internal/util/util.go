// Package util provides utility functions for Watchtower operations.
// Data size parsing uses constants for unit multipliers, adapted from kythe.io datasize package approach.
package util

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Size constants for data size multipliers (following kythe datasize approach).
const (
	// Binary units (powers of 1024).
	kibibyteMultiplier int64 = 1024
	mebibyteMultiplier       = 1024 * kibibyteMultiplier
	gibibyteMultiplier       = 1024 * mebibyteMultiplier
	tebibyteMultiplier       = 1024 * gibibyteMultiplier
	pebibyteMultiplier       = 1024 * tebibyteMultiplier

	// Decimal units (powers of 1000).
	kilobyteMultiplier int64 = 1000
	megabyteMultiplier       = 1000 * kilobyteMultiplier
	gigabyteMultiplier       = 1000 * megabyteMultiplier
	terabyteMultiplier       = 1000 * gigabyteMultiplier
	petabyteMultiplier       = 1000 * terabyteMultiplier

	// Constants for percentage calculations.
	hundredPercent = 100
)

// Static errors for consistent error handling.
var (
	errEmptySize                  = errors.New("empty size")
	errPercentageRequiresMaxSpace = errors.New(
		"percentage sizes require maximum disk space context",
	)
	errPercentageOutOfRange   = errors.New("percentage out of range")
	errUnsupportedBinaryUnit  = errors.New("unsupported binary unit")
	errUnsupportedDecimalUnit = errors.New("unsupported unit")
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

// ParseDiskSpace parses a disk space string into bytes.
//
// It supports various formats like '100MB', '1.5GB', '500MiB', '80%', etc.
// Percentage values require a maximum disk space context to calculate against.
//
// Parameters:
//   - size: The disk space string to parse.
//   - maxSpaceBytes: Maximum disk space in bytes for percentage calculations (0 if not available).
//
// Returns:
//   - int64: The size in bytes.
//   - error: Non-nil if parsing fails.
func ParseDiskSpace(size string, maxSpaceBytes int64) (int64, error) {
	if size == "" {
		return 0, errEmptySize
	}

	// Check if it's a plain number (bytes)
	if val, err := parsePlainNumber(size); err == nil {
		return val, nil
	}

	// Check if it's a percentage
	if strings.HasSuffix(size, "%") {
		return parsePercentage(size, maxSpaceBytes)
	}

	// Parse size with units
	var (
		multiplier int64
		valueStr   string
		err        error
	)

	if strings.HasSuffix(size, "iB") {
		multiplier, valueStr, err = parseBinaryUnits(size)
	} else {
		multiplier, valueStr, err = parseDecimalUnits(size)
	}

	if err != nil {
		return 0, err
	}

	// Parse the numeric value
	value, err := parseNumericValue(valueStr)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value in %s: %w", size, err)
	}

	// Calculate bytes
	bytes := int64(value * float64(multiplier))

	return bytes, nil
}

// parsePlainNumber handles parsing plain integer values representing bytes.
//
// Parameters:
//   - size: The string to parse as an integer.
//
// Returns:
//   - int64: The parsed byte value.
//   - error: Non-nil if parsing fails.
func parsePlainNumber(size string) (int64, error) {
	val, err := strconv.ParseInt(size, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid plain number: %w", err)
	}

	return val, nil
}

// parsePercentage handles parsing percentage values and calculating bytes.
//
// Parameters:
//   - size: The percentage string (e.g., "80%").
//   - maxSpaceBytes: Maximum disk space in bytes for percentage calculations.
//
// Returns:
//   - int64: The calculated byte value.
//   - error: Non-nil if parsing fails or maxSpaceBytes is 0.
func parsePercentage(size string, maxSpaceBytes int64) (int64, error) {
	if maxSpaceBytes == 0 {
		return 0, errPercentageRequiresMaxSpace
	}

	percentStr := strings.TrimSuffix(size, "%")

	percentage, err := strconv.ParseFloat(percentStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid percentage value in %s: %w", size, err)
	}

	if percentage < 0 || percentage > hundredPercent {
		return 0, fmt.Errorf(
			"%w: must be between 0 and %d, got %g",
			errPercentageOutOfRange,
			hundredPercent,
			percentage,
		)
	}

	return int64(float64(maxSpaceBytes) * percentage / hundredPercent), nil
}

// parseBinaryUnits parses binary units (MiB, GiB, etc.) and returns the multiplier and value string.
//
// Parameters:
//   - size: The size string with binary unit.
//
// Returns:
//   - int64: The multiplier for the unit.
//   - string: The numeric value string without the unit.
//   - error: Non-nil if the unit is unsupported.
func parseBinaryUnits(size string) (int64, string, error) {
	switch {
	case strings.HasSuffix(size, "PiB"):
		return pebibyteMultiplier, strings.TrimSuffix(size, "PiB"), nil
	case strings.HasSuffix(size, "TiB"):
		return tebibyteMultiplier, strings.TrimSuffix(size, "TiB"), nil
	case strings.HasSuffix(size, "GiB"):
		return gibibyteMultiplier, strings.TrimSuffix(size, "GiB"), nil
	case strings.HasSuffix(size, "MiB"):
		return mebibyteMultiplier, strings.TrimSuffix(size, "MiB"), nil
	case strings.HasSuffix(size, "KiB"):
		return kibibyteMultiplier, strings.TrimSuffix(size, "KiB"), nil
	default:
		return 0, "", fmt.Errorf("%w in %s", errUnsupportedBinaryUnit, size)
	}
}

// parseDecimalUnits parses decimal units (MB, GB, etc.) and returns the multiplier and value string.
//
// Parameters:
//   - size: The size string with decimal unit.
//
// Returns:
//   - int64: The multiplier for the unit.
//   - string: The numeric value string without the unit.
//   - error: Non-nil if the unit is unsupported.
func parseDecimalUnits(size string) (int64, string, error) {
	switch {
	case strings.HasSuffix(size, "PB"):
		return petabyteMultiplier, strings.TrimSuffix(size, "PB"), nil
	case strings.HasSuffix(size, "P"):
		return petabyteMultiplier, strings.TrimSuffix(size, "P"), nil
	case strings.HasSuffix(size, "TB"):
		return terabyteMultiplier, strings.TrimSuffix(size, "TB"), nil
	case strings.HasSuffix(size, "T"):
		return terabyteMultiplier, strings.TrimSuffix(size, "T"), nil
	case strings.HasSuffix(size, "GB"):
		return gigabyteMultiplier, strings.TrimSuffix(size, "GB"), nil
	case strings.HasSuffix(size, "G"):
		return gigabyteMultiplier, strings.TrimSuffix(size, "G"), nil
	case strings.HasSuffix(size, "MB"):
		return megabyteMultiplier, strings.TrimSuffix(size, "MB"), nil
	case strings.HasSuffix(size, "M"):
		return megabyteMultiplier, strings.TrimSuffix(size, "M"), nil
	case strings.HasSuffix(size, "KB"):
		return kilobyteMultiplier, strings.TrimSuffix(size, "KB"), nil
	case strings.HasSuffix(size, "K"):
		return kilobyteMultiplier, strings.TrimSuffix(size, "K"), nil
	case strings.HasSuffix(size, "B"):
		return 1, strings.TrimSuffix(size, "B"), nil
	default:
		return 0, "", fmt.Errorf("%w in %s", errUnsupportedDecimalUnit, size)
	}
}

// parseNumericValue parses the numeric part of a size string.
//
// Parameters:
//   - valueStr: The string containing the numeric value.
//
// Returns:
//   - float64: The parsed numeric value.
//   - error: Non-nil if parsing fails.
func parseNumericValue(valueStr string) (float64, error) {
	val, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric value: %w", err)
	}

	return val, nil
}
