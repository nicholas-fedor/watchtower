package container

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestClientBuildRemoteImage verifies that the client builds a remote image
// through its own Docker API, and says which step failed when the build fails.
func TestClientBuildRemoteImage(t *testing.T) {
	t.Parallel()

	_, err := (&client{log: newTestLogger(t), api: mockContainer.NewMockAPIClient(t)}).
		BuildRemoteImage(t.Context(), "", "", []string{"app:latest"})
	require.ErrorContains(t, err, "build image from Git: git remote is empty")

	api := newDockerAPI(t)
	api.EXPECT().ImageBuild(mock.Anything, http.NoBody, dockerClient.ImageBuildOptions{
		RemoteContext: "https://github.com/org/app.git",
		Tags:          []string{"app:latest"},
		Remove:        true,
		Dockerfile:    provenance.DefaultDockerfile,
	}).Return(dockerClient.ImageBuildResult{Body: io.NopCloser(strings.NewReader(`{"aux":{"ID":"sha256:built"}}`))}, nil).Once()

	id, err := (&client{log: newTestLogger(t), api: api}).
		BuildRemoteImage(t.Context(), "https://github.com/org/app.git", "", []string{"app:latest"})
	require.NoError(t, err)
	assert.Equal(t, types.ImageID("sha256:built"), id)
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
