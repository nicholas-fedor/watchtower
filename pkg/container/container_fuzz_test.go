package container

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// FuzzNormalizeContainerName verifies that NormalizeContainerName never panics
// and correctly handles leading slashes for any input string.
func FuzzNormalizeContainerName(f *testing.F) {
	f.Add("/my-container")
	f.Add("my-container")
	f.Add("/")
	f.Add("")
	f.Add("///triple-slash")
	f.Add("/container/with/slashes")
	f.Add("container/")
	f.Add("\x00null")

	f.Fuzz(func(t *testing.T, name string) {
		result := NormalizeContainerName(name)

		assert.False(t, strings.HasPrefix(result, "/"),
			"result should not start with /: %q", result)

		if strings.HasPrefix(name, "/") {
			assert.Equal(t, strings.TrimLeft(name, "/"), result,
				"should remove all leading slashes from %q", name)
		} else {
			assert.Equal(t, name, result,
				"should not modify name without leading slash: %q", name)
		}
	})
}
