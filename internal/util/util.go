// Package util provides utility functions for Watchtower operations.
package util

// SliceEqual compares two string slices for equality.
// It returns true if both slices have identical content in the same order.
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

// SliceSubtract removes elements in subtractFrom from slice.
// It returns a new slice containing only elements unique to the first slice.
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

// StringMapSubtract removes matching key-value pairs from map1 based on map2.
// It returns a new map with keys from map1 that are absent or differ in map2.
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

// StructMapSubtract removes keys from map1 that exist in map2.
// It returns a new map with keys unique to map1.
func StructMapSubtract(map1, map2 map[string]struct{}) map[string]struct{} {
	result := map[string]struct{}{}

	for key1, value1 := range map1 {
		if _, ok := map2[key1]; !ok {
			result[key1] = value1
		}
	}

	return result
}
