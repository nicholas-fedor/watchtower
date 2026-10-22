package project

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoad verifies that a project loads from its directory's default Compose
// file, or from explicit files under a given name, and that an empty directory
// is an error.
func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("empty dir", func(t *testing.T) {
		t.Parallel()

		_, err := Load(t.Context(), ProjectRef{})
		require.ErrorIs(t, err, ErrEmptyProjectDir)
	})

	t.Run("default compose file", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
name: webstack
services:
  api:
    build: ./api
  db:
    image: postgres:16
`), 0o600))

		project, err := Load(t.Context(), ProjectRef{Dir: dir})
		require.NoError(t, err)
		assert.Equal(t, "webstack", project.Name)
		assert.ElementsMatch(t, []string{"api", "db"}, project.ServiceNames())
	})

	t.Run("named project and explicit files", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "stack.yml")
		require.NoError(t, os.WriteFile(path, []byte(`
services:
  worker:
    build: ./worker
`), 0o600))

		project, err := Load(t.Context(), ProjectRef{
			Name:        "jobs",
			Dir:         dir,
			ConfigFiles: []string{path},
		})
		require.NoError(t, err)
		assert.Equal(t, "jobs", project.Name)
		assert.Equal(t, []string{"worker"}, project.ServiceNames())
	})
}

// TestLoadIgnoresProcessSecrets verifies that a project interpolates variables
// from its .env file and not from Watchtower's own environment, so Watchtower's
// secrets never reach a service.
//
// The test sets an environment variable, so it does not run in parallel.
func TestLoadIgnoresProcessSecrets(t *testing.T) {
	t.Setenv("WATCHTOWER_GIT_AUTH_TOKEN", "supersecret")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_TAG=fromdotenv\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  api:
    image: app:${APP_TAG}
    environment:
      TOKEN: ${WATCHTOWER_GIT_AUTH_TOKEN}
`), 0o600))

	project, err := Load(t.Context(), ProjectRef{Dir: dir, Name: "p"})
	require.NoError(t, err)

	api, err := project.GetService("api")
	require.NoError(t, err)
	assert.Equal(t, "app:fromdotenv", api.Image)
	assert.NotContains(t, fmt.Sprint(api.Environment), "supersecret")
}

// TestLoadChecksOverrideSymlinks verifies that the default override files,
// which compose-go loads with the default Compose file, get the same symlink
// check: an override that leads outside the project is rejected, and one that
// stays inside it is merged.
func TestLoadChecksOverrideSymlinks(t *testing.T) {
	t.Parallel()

	const base = "services:\n  api:\n    image: app:base\n"

	for _, name := range cli.DefaultOverrideFileNames {
		t.Run("rejects "+name+" leading outside the project", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(base), 0o600))

			outside := filepath.Join(t.TempDir(), "outside.yaml")
			require.NoError(t, os.WriteFile(outside, []byte("services:\n  api:\n    image: app:outside\n"), 0o600))
			require.NoError(t, os.Symlink(outside, filepath.Join(dir, name)))

			_, err := Load(t.Context(), ProjectRef{Dir: dir, Name: "p"})
			require.ErrorIs(t, err, errConfigFile)
		})
	}

	t.Run("merges an override inside the project", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(base), 0o600))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "local"), 0o700))

		target := filepath.Join(dir, "local", "override.yaml")
		require.NoError(t, os.WriteFile(target, []byte("services:\n  api:\n    image: app:override\n"), 0o600))
		require.NoError(t, os.Symlink(target, filepath.Join(dir, "compose.override.yaml")))

		project, err := Load(t.Context(), ProjectRef{Dir: dir, Name: "p"})
		require.NoError(t, err)

		api, err := project.GetService("api")
		require.NoError(t, err)
		assert.Equal(t, "app:override", api.Image)
	})
}

// TestLoadRejectsParentDirectoryProject verifies that a project directory
// without a Compose file is not loaded from a parent directory's Compose file,
// which compose-go finds by searching upward.
func TestLoadRejectsParentDirectoryProject(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(parent, "compose.yaml"), []byte("services:\n  other:\n    image: other:latest\n"), 0o600))

	dir := filepath.Join(parent, "app")
	require.NoError(t, os.Mkdir(dir, 0o700))

	_, err := Load(t.Context(), ProjectRef{Dir: dir, Name: "p"})
	require.ErrorIs(t, err, errConfigFile)
}
