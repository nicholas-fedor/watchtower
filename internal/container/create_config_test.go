package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	dockerContainer "github.com/moby/moby/api/types/container"
)

// TestRestartsAutomatically verifies that every restart policy except "no"
// counts as restarting, and that a container without inspect data counts as
// restarting, since its policy is unknown.
func TestRestartsAutomatically(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info *dockerContainer.InspectResponse
		want bool
	}{
		{name: "no inspect data", info: nil, want: true},
		{name: "no host config", info: &dockerContainer.InspectResponse{}, want: true},
		{name: "empty policy", info: withRestartPolicy(""), want: false},
		{name: "no", info: withRestartPolicy(dockerContainer.RestartPolicyDisabled), want: false},
		{name: "always", info: withRestartPolicy(dockerContainer.RestartPolicyAlways), want: true},
		{name: "unless-stopped", info: withRestartPolicy(dockerContainer.RestartPolicyUnlessStopped), want: true},
		{name: "on-failure", info: withRestartPolicy(dockerContainer.RestartPolicyOnFailure), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, NewContainer(nil, tt.info, nil).RestartsAutomatically())
		})
	}
}

// withRestartPolicy returns inspect data whose host config has the named
// restart policy.
func withRestartPolicy(name dockerContainer.RestartPolicyMode) *dockerContainer.InspectResponse {
	return &dockerContainer.InspectResponse{
		HostConfig: &dockerContainer.HostConfig{RestartPolicy: dockerContainer.RestartPolicy{Name: name}},
	}
}

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
