// Package util provides tests for utility functions used in Watchtower operations.
package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSliceEqual_True verifies that identical slices are considered equal.
// It ensures SliceEqual returns true for matching content.
func TestSliceEqual_True(t *testing.T) {
	t.Parallel()

	slice1 := []string{"a", "b", "c"}
	slice2 := []string{"a", "b", "c"}

	result := SliceEqual(slice1, slice2)

	assert.True(t, result)
}

// TestSliceEqual_DifferentLengths verifies that slices of different lengths are not equal.
// It ensures SliceEqual returns false when lengths differ.
func TestSliceEqual_DifferentLengths(t *testing.T) {
	t.Parallel()

	slice1 := []string{"a", "b", "c"}
	slice2 := []string{"a", "b", "c", "d"}

	result := SliceEqual(slice1, slice2)

	assert.False(t, result)
}

// TestSliceEqual_DifferentContents verifies that slices with different contents are not equal.
// It ensures SliceEqual returns false when elements differ.
func TestSliceEqual_DifferentContents(t *testing.T) {
	t.Parallel()

	slice1 := []string{"a", "b", "c"}
	slice2 := []string{"a", "b", "d"}

	result := SliceEqual(slice1, slice2)

	assert.False(t, result)
}

// TestSliceSubtract verifies that SliceSubtract correctly removes matching elements.
// It ensures the result contains only unique elements from the first slice.
func TestSliceSubtract(t *testing.T) {
	t.Parallel()

	slice := []string{"a", "b", "c"}
	subtractFrom := []string{"a", "c"}

	result := SliceSubtract(slice, subtractFrom)
	assert.Equal(t, []string{"b"}, result)
	assert.Equal(t, []string{"a", "b", "c"}, slice)
	assert.Equal(t, []string{"a", "c"}, subtractFrom)
}

// TestStringMapSubtract verifies that StringMapSubtract removes matching key-value pairs.
// It ensures the result contains only differing or unique entries from the first map.
func TestStringMapSubtract(t *testing.T) {
	t.Parallel()

	map1 := map[string]string{"a": "a", "b": "b", "c": "sea"}
	map2 := map[string]string{"a": "a", "c": "c"}

	result := StringMapSubtract(map1, map2)
	assert.Equal(t, map[string]string{"b": "b", "c": "sea"}, result)
	assert.Equal(t, map[string]string{"a": "a", "b": "b", "c": "sea"}, map1)
	assert.Equal(t, map[string]string{"a": "a", "c": "c"}, map2)
}

// TestStructMapSubtract verifies that StructMapSubtract removes matching keys.
// It ensures the result contains only keys unique to the first map.
func TestStructMapSubtract(t *testing.T) {
	t.Parallel()

	emptyStruct := struct{}{}
	map1 := map[string]struct{}{"a": emptyStruct, "b": emptyStruct, "c": emptyStruct}
	map2 := map[string]struct{}{"a": emptyStruct, "c": emptyStruct}

	result := StructMapSubtract(map1, map2)
	assert.Equal(t, map[string]struct{}{"b": emptyStruct}, result)
	assert.Equal(t, map[string]struct{}{"a": emptyStruct, "b": emptyStruct, "c": emptyStruct}, map1)
	assert.Equal(t, map[string]struct{}{"a": emptyStruct, "c": emptyStruct}, map2)
}

// TestGenerateRandomSHA256 verifies that GenerateRandomSHA256 produces a 64-character string.
// It ensures the result is the correct length and lacks a prefix.
func TestGenerateRandomSHA256(t *testing.T) {
	t.Parallel()

	result := GenerateRandomSHA256()
	assert.Len(t, result, 64)
	assert.NotContains(t, result, "sha256:")
}

// TestGenerateRandomPrefixedSHA256 verifies that GenerateRandomPrefixedSHA256 produces a valid hash.
// It ensures the result matches the expected SHA-256 prefixed format.
func TestGenerateRandomPrefixedSHA256(t *testing.T) {
	t.Parallel()

	result := GenerateRandomPrefixedSHA256()
	assert.Regexp(t, "sha256:[0-9|a-f]{64}", result)
}

// TestRandName verifies that RandName generates a valid random container name.
// It ensures the name is 32 characters long and contains only alphabetic characters.
func TestRandName(t *testing.T) {
	t.Parallel()

	name := RandName()

	// Check length is exactly 32 characters.
	assert.Len(
		t,
		name,
		randomNameLength,
		"RandName should generate a %d-character name",
		randomNameLength,
	)

	// Check that the name matches the expected pattern of only letters.
	assert.Regexp(t, "^[a-zA-Z]+$", name, "RandName should contain only alphabetic characters")

	// Verify uniqueness by generating another name and checking they differ.
	anotherName := RandName()
	assert.NotEqual(t, name, anotherName, "RandName should generate unique names")
}

// TestMinInt_FirstSmaller verifies that MinInt returns the smaller value when the first argument is smaller.
func TestMinInt_FirstSmaller(t *testing.T) {
	t.Parallel()

	result := MinInt(3, 5)
	assert.Equal(t, 3, result)
}

// TestMinInt_SecondSmaller verifies that MinInt returns the smaller value when the second argument is smaller.
func TestMinInt_SecondSmaller(t *testing.T) {
	t.Parallel()

	result := MinInt(7, 2)
	assert.Equal(t, 2, result)
}

// TestMinInt_Equal verifies that MinInt returns either value when both arguments are equal.
func TestMinInt_Equal(t *testing.T) {
	t.Parallel()

	result := MinInt(4, 4)
	assert.Equal(t, 4, result)
}

// TestMinInt_NegativeNumbers verifies that MinInt works correctly with negative numbers.
func TestMinInt_NegativeNumbers(t *testing.T) {
	t.Parallel()

	result := MinInt(-1, -3)
	assert.Equal(t, -3, result)
}

// TestMinInt_Zero verifies that MinInt works correctly with zero.
func TestMinInt_Zero(t *testing.T) {
	t.Parallel()

	result := MinInt(0, 5)
	assert.Equal(t, 0, result)
}

// TestFormatDuration_Zero verifies that FormatDuration returns "0 seconds" for zero duration.
func TestFormatDuration_Zero(t *testing.T) {
	t.Parallel()

	result := FormatDuration(0)
	assert.Equal(t, "0 seconds", result)
}

// TestFormatDuration_SecondsOnly verifies that FormatDuration formats seconds correctly.
func TestFormatDuration_SecondsOnly(t *testing.T) {
	t.Parallel()

	result := FormatDuration(45 * time.Second)
	assert.Equal(t, "45 seconds", result)
}

// TestFormatDuration_MinutesAndSeconds verifies that FormatDuration formats minutes and seconds correctly.
func TestFormatDuration_MinutesAndSeconds(t *testing.T) {
	t.Parallel()

	result := FormatDuration(2*time.Minute + 30*time.Second)
	assert.Equal(t, "2 minutes, 30 seconds", result)
}

// TestFormatDuration_HoursMinutesSeconds verifies that FormatDuration formats hours, minutes, and seconds correctly.
func TestFormatDuration_HoursMinutesSeconds(t *testing.T) {
	t.Parallel()

	result := FormatDuration(1*time.Hour + 15*time.Minute + 45*time.Second)
	assert.Equal(t, "1 hour, 15 minutes, 45 seconds", result)
}

// TestFormatDuration_SingleValues verifies that FormatDuration uses singular forms for single units.
func TestFormatDuration_SingleValues(t *testing.T) {
	t.Parallel()

	result := FormatDuration(1*time.Hour + 1*time.Minute + 1*time.Second)
	assert.Equal(t, "1 hour, 1 minute, 1 second", result)
}

// TestFormatDuration_LargeDuration verifies that FormatDuration handles large durations correctly.
func TestFormatDuration_LargeDuration(t *testing.T) {
	t.Parallel()

	result := FormatDuration(25*time.Hour + 30*time.Minute)
	assert.Equal(t, "25 hours, 30 minutes", result)
}

// TestFormatTimeUnit_SingleValues verifies that FormatTimeUnit uses singular forms for single units.
func TestFormatTimeUnit_SingleValues(t *testing.T) {
	t.Parallel()

	result := FormatTimeUnit(1, "hour", "hours", false)
	assert.Equal(t, "1 hour", result)

	result = FormatTimeUnit(1, "minute", "minutes", false)
	assert.Equal(t, "1 minute", result)

	result = FormatTimeUnit(1, "second", "seconds", false)
	assert.Equal(t, "1 second", result)
}

// TestFormatTimeUnit_PluralValues verifies that FormatTimeUnit uses plural forms for multiple units.
func TestFormatTimeUnit_PluralValues(t *testing.T) {
	t.Parallel()

	result := FormatTimeUnit(2, "hour", "hours", false)
	assert.Equal(t, "2 hours", result)

	result = FormatTimeUnit(5, "minute", "minutes", false)
	assert.Equal(t, "5 minutes", result)
}

// TestFormatTimeUnit_ZeroNotForced verifies that FormatTimeUnit returns empty string for zero values when not forced.
func TestFormatTimeUnit_ZeroNotForced(t *testing.T) {
	t.Parallel()

	result := FormatTimeUnit(0, "hour", "hours", false)
	assert.Empty(t, result)
}

// TestFormatTimeUnit_ZeroForced verifies that FormatTimeUnit returns formatted string for zero values when forced.
func TestFormatTimeUnit_ZeroForced(t *testing.T) {
	t.Parallel()

	result := FormatTimeUnit(0, "second", "seconds", true)
	assert.Equal(t, "0 seconds", result)
}

// TestFilterEmpty_Mixed verifies that FilterEmpty removes empty strings and keeps non-empty ones.
func TestFilterEmpty_Mixed(t *testing.T) {
	t.Parallel()

	input := []string{"1 hour", "", "30 minutes", "", "45 seconds"}
	result := FilterEmpty(input)
	assert.Equal(t, []string{"1 hour", "30 minutes", "45 seconds"}, result)
}

// TestFilterEmpty_AllEmpty verifies that FilterEmpty returns empty slice when all inputs are empty.
func TestFilterEmpty_AllEmpty(t *testing.T) {
	t.Parallel()

	input := []string{"", "", ""}
	result := FilterEmpty(input)
	assert.Equal(t, []string(nil), result)
}

// TestFilterEmpty_NoEmpty verifies that FilterEmpty returns all elements when none are empty.
func TestFilterEmpty_NoEmpty(t *testing.T) {
	t.Parallel()

	input := []string{"1 hour", "30 minutes", "45 seconds"}
	result := FilterEmpty(input)
	assert.Equal(t, []string{"1 hour", "30 minutes", "45 seconds"}, result)
}

// TestFilterEmpty_EmptyInput verifies that FilterEmpty returns empty slice for empty input.
func TestFilterEmpty_EmptyInput(t *testing.T) {
	t.Parallel()

	input := []string{}
	result := FilterEmpty(input)
	assert.Equal(t, []string(nil), result)
}

// TestNormalizeContainerName_WithLeadingSlash verifies that NormalizeContainerName removes leading slash.
func TestNormalizeContainerName_WithLeadingSlash(t *testing.T) {
	t.Parallel()

	result := NormalizeContainerName("/test-container")
	assert.Equal(t, "test-container", result)
}

// TestNormalizeContainerName_WithoutLeadingSlash verifies that NormalizeContainerName leaves names without leading slash unchanged.
func TestNormalizeContainerName_WithoutLeadingSlash(t *testing.T) {
	t.Parallel()

	result := NormalizeContainerName("test-container")
	assert.Equal(t, "test-container", result)
}

// TestNormalizeContainerName_EmptyString verifies that NormalizeContainerName handles empty strings.
func TestNormalizeContainerName_EmptyString(t *testing.T) {
	t.Parallel()

	result := NormalizeContainerName("")
	assert.Empty(t, result)
}

// TestNormalizeContainerName_OnlySlash verifies that NormalizeContainerName handles strings with only a slash.
func TestNormalizeContainerName_OnlySlash(t *testing.T) {
	t.Parallel()

	result := NormalizeContainerName("/")
	assert.Empty(t, result)
}

// TestNormalizeContainerName_MultipleLeadingSlashes verifies that NormalizeContainerName removes only the first leading slash.
func TestNormalizeContainerName_MultipleLeadingSlashes(t *testing.T) {
	t.Parallel()

	result := NormalizeContainerName("//test-container")
	assert.Equal(t, "/test-container", result)
}

// TestNormalizeContainerName_SlashInMiddle verifies that NormalizeContainerName only removes leading slashes.
func TestNormalizeContainerName_SlashInMiddle(t *testing.T) {
	t.Parallel()

	result := NormalizeContainerName("test/container")
	assert.Equal(t, "test/container", result)
}
