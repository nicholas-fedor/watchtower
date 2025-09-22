// Package util provides tests for utility functions used in Watchtower operations.
package util

import (
	"testing"

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
