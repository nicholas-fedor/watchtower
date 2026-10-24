package container

import (
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// FuzzConfinedDockerfile verifies Dockerfile labels cannot escape the context.
func FuzzConfinedDockerfile(f *testing.F) {
	f.Add("")
	f.Add("Dockerfile")
	f.Add("build/docker/Dockerfile")
	f.Add("../Dockerfile")
	f.Add("/etc/passwd")
	f.Add(`..\windows`)

	f.Fuzz(func(t *testing.T, dockerfile string) {
		got, err := confinedDockerfile(dockerfile)
		if err != nil {
			assert.ErrorIs(t, err, errGitDockerfileEscape)

			return
		}

		assert.NotEmpty(t, got)
		assert.False(t, path.IsAbs(got))
		assert.True(t, filepath.IsLocal(filepath.FromSlash(got)))
	})
}
