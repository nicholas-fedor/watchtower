package project

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
