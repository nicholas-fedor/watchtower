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

// FuzzParseBind verifies that ParseBind never panics. It also checks that a
// parsed entry has a non-empty source and a destination without a colon and
// rebuilds the input. Seeds cover host paths, volumes, options, a Windows
// drive letter, and entries without a source or destination.
func FuzzParseBind(f *testing.F) {
	f.Add("/host/config.yaml:/app/config.yaml:ro")
	f.Add("cache:/data")
	f.Add(`C:\data:/data:rw,z`)
	f.Add("nocolon")
	f.Add("::")
	f.Add("C:")
	f.Add("")

	f.Fuzz(func(t *testing.T, bind string) {
		got, ok := ParseBind(bind)
		if !ok {
			return
		}

		if got.Source == "" || got.Destination == "" || strings.Contains(got.Destination, ":") {
			t.Fatalf("ParseBind(%q) = %+v", bind, got)
		}

		rebuilt := got.Source + ":" + got.Destination
		if got.Options != "" || strings.HasSuffix(bind, ":") {
			rebuilt += ":" + got.Options
		}

		if rebuilt != bind {
			t.Fatalf("ParseBind(%q) = %+v, which rebuilds %q", bind, got, rebuilt)
		}
	})
}
