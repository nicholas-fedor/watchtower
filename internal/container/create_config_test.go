package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSliceSubtract verifies that SliceSubtract correctly removes matching elements.
// It ensures the result contains only unique elements from the first slice.
func TestSliceSubtract(t *testing.T) {
	t.Parallel()

	slice := []string{"a", "b", "c"}
	subtractFrom := []string{"a", "c"}

	result := sliceSubtract(slice, subtractFrom)
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

	result := stringMapSubtract(map1, map2)
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

	result := structMapSubtract(map1, map2)
	assert.Equal(t, map[string]struct{}{"b": emptyStruct}, result)
	assert.Equal(t, map[string]struct{}{"a": emptyStruct, "b": emptyStruct, "c": emptyStruct}, map1)
	assert.Equal(t, map[string]struct{}{"a": emptyStruct, "c": emptyStruct}, map2)
}
