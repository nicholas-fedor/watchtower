package container

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	mockContainer "github.com/nicholas-fedor/watchtower/pkg/container/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestClientBuildRemoteImage verifies that a remote build sends the Git context,
// tags, and Dockerfile to Docker, takes the image ID from the build stream or
// else from an inspect of the first tag, and keeps credentials out of its
// errors.
func TestClientBuildRemoteImage(t *testing.T) {
	t.Parallel()

	const remote = "https://user:secret@github.com/org/app.git#abc"

	// expectBuild expects one build of remote tagged app:latest and answers it
	// with the given stream body.
	expectBuild := func(api *mockContainer.MockAPIClient, body string, err error) {
		api.EXPECT().ImageBuild(mock.Anything, http.NoBody, dockerClient.ImageBuildOptions{
			RemoteContext: remote,
			Tags:          []string{"app:latest"},
			Remove:        true,
			Dockerfile:    DefaultGitDockerfile,
		}).Return(dockerClient.ImageBuildResult{Body: io.NopCloser(strings.NewReader(body))}, err).Once()
	}

	t.Run("returns the image ID from the build stream", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		expectBuild(api, `{"aux":{"ID":"sha256:built"}}`, nil)

		id, err := (&client{log: newTestLogger(t), api: api}).BuildRemoteImage(t.Context(), remote, "", []string{"app:latest"})
		require.NoError(t, err)
		assert.Equal(t, types.ImageID("sha256:built"), id)
	})

	t.Run("inspects the first tag when the stream has no image ID", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		expectBuild(api, `{"stream":"Step 1/1"}`, nil)
		api.EXPECT().ImageInspect(mock.Anything, "app:latest").Return(inspected("sha256:tagged"), nil).Once()

		id, err := (&client{log: newTestLogger(t), api: api}).BuildRemoteImage(t.Context(), remote, "", []string{"app:latest"})
		require.NoError(t, err)
		assert.Equal(t, types.ImageID("sha256:tagged"), id)
	})

	t.Run("fails when no image ID can be found", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		expectBuild(api, `{"stream":"Step 1/1"}`, nil)
		api.EXPECT().ImageInspect(mock.Anything, "app:latest").Return(inspected(""), nil).Once()

		_, err := (&client{log: newTestLogger(t), api: api}).BuildRemoteImage(t.Context(), remote, "", []string{"app:latest"})
		require.ErrorIs(t, err, errBuildImageIDMissing)
	})

	t.Run("redacts credentials from a failed build", func(t *testing.T) {
		t.Parallel()

		api := newDockerAPI(t)
		expectBuild(api, "", fmt.Errorf("unable to prepare context: %s", remote))

		_, err := (&client{log: newTestLogger(t), api: api}).BuildRemoteImage(t.Context(), remote, "", []string{"app:latest"})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "secret")
	})

	t.Run("redacts a query-escaped remote from a failed build", func(t *testing.T) {
		t.Parallel()

		// The client sends the remote as a query parameter, so its error can
		// echo the request URL with the remote query-escaped.
		api := newDockerAPI(t)
		expectBuild(api, "", fmt.Errorf(`Post "http://docker/build?remote=%s": EOF`, url.QueryEscape(remote)))

		_, err := (&client{log: newTestLogger(t), api: api}).BuildRemoteImage(t.Context(), remote, "", []string{"app:latest"})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "secret")
		assert.Contains(t, err.Error(), url.QueryEscape("https://xxxxx:xxxxx@github.com"))
	})

	t.Run("rejects an empty remote before calling Docker", func(t *testing.T) {
		t.Parallel()

		// The mock has no expectations, so any Docker call fails the test.
		_, err := (&client{log: newTestLogger(t), api: mockContainer.NewMockAPIClient(t)}).
			BuildRemoteImage(t.Context(), "", "", []string{"app:latest"})
		require.ErrorIs(t, err, errGitRemoteEmpty)
	})
}

func TestDiscardStoredCopyFiles(t *testing.T) {
	t.Parallel()

	sourceID := types.ContainerID("source-id")
	otherID := types.ContainerID("other-id")
	cli := &client{
		copyFiles: pendingCopyFiles{
			snapshots: map[types.ContainerID]copyFileSnapshot{
				sourceID: {files: []copyFileEntry{{target: "/app/config.yaml"}}},
				otherID:  {files: []copyFileEntry{{target: "/app/extra.conf"}}},
			},
		},
	}

	cli.DiscardCopyFiles(sourceID)
	assert.NotContains(t, cli.copyFiles.snapshots, sourceID)
	require.Contains(t, cli.copyFiles.snapshots, otherID)
	assert.Equal(t, "/app/extra.conf", cli.copyFiles.snapshots[otherID].files[0].target)

	cli.DiscardCopyFiles(sourceID)
}

func TestClientCopyFileStore(t *testing.T) {
	t.Parallel()

	t.Run("snapshot empty label is a no-op", func(t *testing.T) {
		t.Parallel()

		api := mockContainer.NewMockAPIClient(t)
		cli := &client{log: newTestLogger(t), api: api}
		require.NoError(t, cli.SnapshotCopyFiles(t.Context(), MockContainer()))
		assert.Empty(t, cli.copyFiles.snapshots)
	})

	t.Run("snapshot stores sanitized file", func(t *testing.T) {
		t.Parallel()

		tarBytes := mustFileTar(t, "config.yaml", []byte("setting=1"))
		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().
			CopyFromContainer(mock.Anything, "container_id", copyFromPath("/app/config.yaml")).
			Return(dockerClient.CopyFromContainerResult{
				Content: io.NopCloser(bytes.NewReader(tarBytes)),
				Stat: dockerContainer.PathStat{
					Name: "config.yaml",
					Size: int64(len("setting=1")),
					Mode: 0o444,
				},
			}, nil)

		cli := &client{log: newTestLogger(t), api: api}
		source := MockContainer(containerfixture.WithLabels(map[string]string{
			labels.CopyFile: "/app/config.yaml",
		}))
		require.NoError(t, cli.SnapshotCopyFiles(t.Context(), source))
		require.Contains(t, cli.copyFiles.snapshots, source.ID())
		assert.Positive(t, cli.copyFiles.bytes)
	})

	t.Run("snapshot invalid label fails", func(t *testing.T) {
		t.Parallel()

		cli := &client{log: newTestLogger(t), api: mockContainer.NewMockAPIClient(t)}
		err := cli.SnapshotCopyFiles(t.Context(), MockContainer(containerfixture.WithLabels(map[string]string{
			labels.CopyFile: "relative.conf",
		})))
		require.ErrorIs(t, err, errCopyFileInvalidPath)
	})

	t.Run("inject missing snapshot is a no-op", func(t *testing.T) {
		t.Parallel()

		cli := &client{log: newTestLogger(t), api: mockContainer.NewMockAPIClient(t)}
		require.NoError(t, cli.injectStoredCopyFiles(t.Context(), MockContainer(), "new-id"))
	})

	t.Run("inject writes then drops snapshot", func(t *testing.T) {
		t.Parallel()

		tarBytes := mustFileTar(t, "config.yaml", []byte("setting=1"))
		source := MockContainer()
		api := mockContainer.NewMockAPIClient(t)
		expectDaemonInfo(api)
		api.EXPECT().
			CopyToContainer(mock.Anything, "new-id", mock.Anything).
			Return(dockerClient.CopyToContainerResult{}, nil)

		cli := &client{
			log: newTestLogger(t),
			api: api,
			copyFiles: pendingCopyFiles{
				snapshots: map[types.ContainerID]copyFileSnapshot{
					source.ID(): {files: []copyFileEntry{{
						target:  "/app/config.yaml",
						archive: tarBytes,
					}}},
				},
				bytes: int64(len(tarBytes)),
			},
		}
		require.NoError(t, cli.injectStoredCopyFiles(t.Context(), source, "new-id"))
		assert.Empty(t, cli.copyFiles.snapshots)
		assert.Equal(t, int64(0), cli.copyFiles.bytes)
	})

	t.Run("inject failure still drops snapshot", func(t *testing.T) {
		t.Parallel()

		source := MockContainer()
		api := mockContainer.NewMockAPIClient(t)
		expectDaemonInfo(api)
		api.EXPECT().
			CopyToContainer(mock.Anything, "new-id", mock.Anything).
			Return(dockerClient.CopyToContainerResult{}, errors.New("denied"))

		cli := &client{
			log: newTestLogger(t),
			api: api,
			copyFiles: pendingCopyFiles{
				snapshots: map[types.ContainerID]copyFileSnapshot{
					source.ID(): {files: []copyFileEntry{{
						target:  "/app/config.yaml",
						archive: []byte("tar"),
					}}},
				},
				bytes: 3,
			},
		}
		err := cli.injectStoredCopyFiles(t.Context(), source, "new-id")
		require.ErrorIs(t, err, errCopyFileInjectFailed)
		assert.Empty(t, cli.copyFiles.snapshots)
		assert.Equal(t, int64(0), cli.copyFiles.bytes)
	})

	t.Run("store clamps negative byte count", func(t *testing.T) {
		t.Parallel()

		pending := &pendingCopyFiles{bytes: -8}
		require.NoError(t, pending.store("source-id", copyFileSnapshot{
			files: []copyFileEntry{{archive: []byte("tar")}},
		}))
		assert.Equal(t, int64(3), pending.bytes)
	})

	t.Run("discard clamps negative byte count", func(t *testing.T) {
		t.Parallel()

		cli := &client{
			copyFiles: pendingCopyFiles{
				snapshots: map[types.ContainerID]copyFileSnapshot{
					"source-id": {files: []copyFileEntry{{archive: []byte("tar")}}},
				},
			},
		}
		cli.DiscardCopyFiles("source-id")
		assert.Equal(t, int64(0), cli.copyFiles.bytes)
		assert.Empty(t, cli.copyFiles.snapshots)
	})
}
