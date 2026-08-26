package compose

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// FuzzConfigFiles verifies Compose config_files labels never panic and that
// results contain no empty paths.
func FuzzConfigFiles(f *testing.F) {
	f.Add("compose.yaml,/abs/file.yml,nested/app.yml")
	f.Add(",  ,compose.yaml,")
	f.Add("")
	f.Add("compose.yaml")
	f.Add("/abs/only.yml")

	f.Fuzz(func(t *testing.T, raw string) {
		got := configFiles(map[string]string{ComposeConfigFilesLabel: raw}, "/proj")
		for _, path := range got {
			assert.NotEmpty(t, path)

			if filepath.IsAbs(path) {
				continue
			}

			t.Fatalf("expected absolute path, got %q", path)
		}
	})
}
