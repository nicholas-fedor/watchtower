package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProjectDir(t *testing.T) {
	t.Parallel()

	nop := zerolog.Nop()

	t.Run("compose-dir wins over the process map", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeDir(t, "compose.yaml")
		other := writeComposeDir(t, "compose.yaml")

		ref, err := ResolveProjectDir(&nop, map[string]string{
			WatchtowerComposeDirLabel: dir,
			ComposeProjectLabel:       "web",
			ComposeWorkingDirLabel:    other,
		}, map[string]string{"web": other})
		require.NoError(t, err)
		assert.Equal(t, "web", ref.Name)
		assert.Equal(t, dir, ref.Dir)
	})

	t.Run("unreadable compose-dir is an error", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "missing")
		dir := writeComposeDir(t, "compose.yaml")
		_, err := ResolveProjectDir(&nop, map[string]string{
			WatchtowerComposeDirLabel: missing,
			ComposeProjectLabel:       "web",
		}, map[string]string{"web": dir})
		require.ErrorIs(t, err, ErrProjectDir)
	})

	t.Run("process map when compose-dir is unset", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeDir(t, "compose.yaml")
		ref, err := ResolveProjectDir(&nop, map[string]string{
			ComposeProjectLabel:    "web",
			ComposeWorkingDirLabel: writeComposeDir(t, "compose.yaml"),
		}, map[string]string{"web": dir})
		require.NoError(t, err)
		assert.Equal(t, dir, ref.Dir)
	})

	t.Run("unreadable process map is an error", func(t *testing.T) {
		t.Parallel()

		missing := filepath.Join(t.TempDir(), "missing")
		_, err := ResolveProjectDir(&nop, map[string]string{
			ComposeProjectLabel:    "web",
			ComposeWorkingDirLabel: writeComposeDir(t, "compose.yaml"),
		}, map[string]string{"web": missing})
		require.ErrorIs(t, err, ErrProjectDir)
	})

	t.Run("working dir alone does not opt in", func(t *testing.T) {
		t.Parallel()

		ref, err := ResolveProjectDir(&nop, map[string]string{
			ComposeWorkingDirLabel: writeComposeDir(t, "compose.yaml"),
		}, nil)
		require.NoError(t, err)
		assert.Empty(t, ref.Dir)
	})

	t.Run("nil labels", func(t *testing.T) {
		t.Parallel()

		ref, err := ResolveProjectDir(&nop, nil, map[string]string{"web": writeComposeDir(t, "compose.yaml")})
		require.NoError(t, err)
		assert.Empty(t, ref.Dir)
	})

	t.Run("whitespace compose-dir is ignored", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeDir(t, "compose.yaml")
		ref, err := ResolveProjectDir(&nop, map[string]string{
			WatchtowerComposeDirLabel: "  ",
			ComposeProjectLabel:       "web",
		}, map[string]string{"web": dir})
		require.NoError(t, err)
		assert.Equal(t, dir, ref.Dir)
	})

	t.Run("git labels only are not a compose project", func(t *testing.T) {
		t.Parallel()

		ref, err := ResolveProjectDir(&nop, map[string]string{
			"com.centurylinklabs.watchtower.git-repo": "https://git.example.com/org/app.git",
		}, nil)
		require.NoError(t, err)
		assert.Empty(t, ref.Dir)
	})

	t.Run("includes config files", func(t *testing.T) {
		t.Parallel()

		dir := writeComposeDir(t, "compose.yaml")
		abs := filepath.Join(t.TempDir(), "override.yaml")
		ref, err := ResolveProjectDir(&nop, map[string]string{
			WatchtowerComposeDirLabel: dir,
			ComposeConfigFilesLabel:   "compose.yaml,  extra.yml, " + abs + ", ,",
		}, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{
			filepath.Join(dir, "compose.yaml"),
			filepath.Join(dir, "extra.yml"),
			abs,
		}, ref.ConfigFiles)
	})
}

func TestConfigFiles(t *testing.T) {
	t.Parallel()

	t.Run("absent label", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, configFiles(map[string]string{}, "/proj"))
		assert.Nil(t, configFiles(nil, "/proj"))
	})

	t.Run("relative and absolute", func(t *testing.T) {
		t.Parallel()

		got := configFiles(map[string]string{
			ComposeConfigFilesLabel: "compose.yaml,/abs/file.yml,  nested/app.yml",
		}, "/proj")
		assert.Equal(t, []string{"/proj/compose.yaml", "/abs/file.yml", "/proj/nested/app.yml"}, got)
	})

	t.Run("skips blanks", func(t *testing.T) {
		t.Parallel()

		got := configFiles(map[string]string{
			ComposeConfigFilesLabel: ",  ,compose.yaml,",
		}, "/proj")
		assert.Equal(t, []string{"/proj/compose.yaml"}, got)
	})
}

func TestReadableProjectDir(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.True(t, readableProjectDir(writeComposeDir(t, name)))
		})
	}

	t.Run("missing path", func(t *testing.T) {
		t.Parallel()

		assert.False(t, readableProjectDir(filepath.Join(t.TempDir(), "missing")))
	})

	t.Run("file is not a directory", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "compose.yaml")
		require.NoError(t, os.WriteFile(path, []byte("services: {}\n"), 0o600))
		assert.False(t, readableProjectDir(path))
	})

	t.Run("directory without compose file", func(t *testing.T) {
		t.Parallel()

		assert.False(t, readableProjectDir(t.TempDir()))
	})
}

func TestLabelValue(t *testing.T) {
	t.Parallel()

	assert.Empty(t, labelValue(nil, WatchtowerComposeDirLabel))
	assert.Empty(t, labelValue(map[string]string{}, WatchtowerComposeDirLabel))
	assert.Equal(t, "/proj", labelValue(map[string]string{WatchtowerComposeDirLabel: "/proj"}, WatchtowerComposeDirLabel))
	assert.Equal(t, "/proj", labelValue(map[string]string{WatchtowerComposeDirLabel: "  /proj  "}, WatchtowerComposeDirLabel))
}

func TestFileExists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "compose.yaml")
	require.NoError(t, os.WriteFile(path, []byte("x\n"), 0o600))
	assert.True(t, fileExists(path))
	assert.False(t, fileExists(filepath.Join(t.TempDir(), "missing")))
	assert.False(t, fileExists(t.TempDir()))
}

// writeComposeDir creates a project directory containing filename.
//
// Parameters:
//   - t: Test handle.
//   - filename: Compose file name to write.
//
// Returns:
//   - string: Project directory path.
func writeComposeDir(t *testing.T, filename string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte("services: {}\n"), 0o600))

	return dir
}
