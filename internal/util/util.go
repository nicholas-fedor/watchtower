// Package util provides utility functions for Watchtower operations.
package util

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

// SliceEqual checks if two string slices are identical.
//
// Parameters:
//   - slice1: First slice.
//   - slice2: Second slice.
//
// Returns:
//   - bool: True if equal, false otherwise.
func SliceSubtract(slice, subtractFrom []string) []string {
	result := []string{}

	for _, element1 := range slice {
		found := false

		for _, element2 := range subtractFrom {
			if element1 == element2 {
				found = true

				break
			}
		}

		if !found {
			result = append(result, element1)
		}
	}

	return result
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
