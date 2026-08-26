package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("empty dir", func(t *testing.T) {
		t.Parallel()

		_, err := Load(t.Context(), ProjectRef{})
		require.ErrorIs(t, err, errEmptyProjectDir)
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

func TestInjectLabels(t *testing.T) {
	t.Parallel()

	t.Run("nil project", func(t *testing.T) {
		t.Parallel()

		_, err := InjectLabels(nil, map[string]map[string]string{"api": {"k": "v"}})
		require.ErrorIs(t, err, errNilProject)
	})

	t.Run("empty labels returns the same project", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte("services:\n  api:\n    image: app\n"), 0o600))

		project, err := Load(t.Context(), ProjectRef{Dir: dir, Name: "p"})
		require.NoError(t, err)

		got, err := InjectLabels(project, nil)
		require.NoError(t, err)
		assert.Equal(t, project, got)
	})

	t.Run("merges labels onto the named service only", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(`
services:
  api:
    build: ./api
    labels:
      keep: "yes"
  db:
    image: postgres:16
`), 0o600))

		project, err := Load(t.Context(), ProjectRef{Dir: dir, Name: "p"})
		require.NoError(t, err)

		got, err := InjectLabels(project, map[string]map[string]string{
			"api": {
				"com.centurylinklabs.watchtower.git-last-commit": "abc",
			},
		})
		require.NoError(t, err)

		api, err := got.GetService("api")
		require.NoError(t, err)
		assert.Equal(t, "yes", api.Labels["keep"])
		assert.Equal(t, "abc", api.Labels["com.centurylinklabs.watchtower.git-last-commit"])

		db, err := got.GetService("db")
		require.NoError(t, err)
		assert.Empty(t, db.Labels["com.centurylinklabs.watchtower.git-last-commit"])
	})
}

func TestAppliedImageID(t *testing.T) {
	t.Parallel()

	summary := api.ContainerSummary{Name: "webstack-api-1", Image: "webstack-api:latest"}
	assert.Empty(t, appliedImageID(summary, nil))
	assert.Empty(t, appliedImageID(summary, map[string]api.ImageSummary{}))
	assert.Equal(t, types.ImageID("sha256:abc"), appliedImageID(
		summary,
		map[string]api.ImageSummary{"webstack-api-1": {ID: "sha256:abc"}},
	))
	assert.Equal(
		t,
		types.ImageID("sha256:fromps"),
		appliedImageID(api.ContainerSummary{Name: "x", Image: "sha256:fromps"}, nil),
	)
}

func TestSDKApplyValidation(t *testing.T) {
	t.Parallel()

	sdk := NewSDK()

	_, err := sdk.Apply(t.Context(), Request{})
	require.ErrorIs(t, err, errEmptyProjectDir)

	_, err = sdk.Apply(t.Context(), Request{Ref: ProjectRef{Dir: t.TempDir()}})
	require.ErrorIs(t, err, errNoComposeServices)
}
