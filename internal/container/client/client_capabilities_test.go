package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerClient "github.com/moby/moby/client"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	mockContainer "github.com/nicholas-fedor/watchtower/internal/container/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
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

// TestClientCopyFileStore verifies that the client snapshots labeled files
// before a recreation and injects them into the replacement. The snapshot is
// dropped whether or not the injection succeeds.
func TestClientCopyFileStore(t *testing.T) {
	t.Parallel()

	// configSource returns a container whose copy-file label names one file.
	configSource := func() *container.Container {
		return modelfixture.Container(containerfixture.WithLabels(map[string]string{
			labels.CopyFile: "/app/config.yaml",
		}))
	}

	// expectConfigCopy expects the snapshot of the labeled file.
	expectConfigCopy := func(t *testing.T, api *mockContainer.MockAPIClient) {
		t.Helper()

		tarBytes := mustFileTar(t, "config.yaml", []byte("setting=1"))
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
	}

	t.Run("snapshot empty label is a no-op", func(t *testing.T) {
		t.Parallel()

		cli := &client{log: newTestLogger(t), api: mockContainer.NewMockAPIClient(t)}
		source := modelfixture.Container()
		require.NoError(t, cli.SnapshotCopyFiles(t.Context(), source))

		_, stored := cli.copyFiles.Take(source.ID())
		assert.False(t, stored)
	})

	t.Run("snapshot stores sanitized file", func(t *testing.T) {
		t.Parallel()

		api := mockContainer.NewMockAPIClient(t)
		expectConfigCopy(t, api)

		cli := &client{log: newTestLogger(t), api: api}
		source := configSource()
		require.NoError(t, cli.SnapshotCopyFiles(t.Context(), source))

		snapshot, stored := cli.copyFiles.Take(source.ID())
		require.True(t, stored)
		assert.False(t, snapshot.Empty())
	})

	t.Run("snapshot invalid label fails", func(t *testing.T) {
		t.Parallel()

		cli := &client{log: newTestLogger(t), api: mockContainer.NewMockAPIClient(t)}
		err := cli.SnapshotCopyFiles(t.Context(), modelfixture.Container(containerfixture.WithLabels(map[string]string{
			labels.CopyFile: "relative.conf",
		})))
		require.ErrorContains(t, err, "invalid copy-file path")
	})

	t.Run("snapshot that overflows the store fails", func(t *testing.T) {
		t.Parallel()

		// Each container's file is just under the per-file limit, so the
		// store fills after about 16 of them.
		body := bytes.Repeat([]byte("x"), 1<<20-1024)
		tarBytes := mustFileTar(t, "config.yaml", body)

		api := mockContainer.NewMockAPIClient(t)
		api.EXPECT().
			CopyFromContainer(mock.Anything, mock.Anything, copyFromPath("/app/config.yaml")).
			RunAndReturn(func(context.Context, string, dockerClient.CopyFromContainerOptions) (dockerClient.CopyFromContainerResult, error) {
				return dockerClient.CopyFromContainerResult{
					Content: io.NopCloser(bytes.NewReader(tarBytes)),
					Stat:    dockerContainer.PathStat{Name: "config.yaml", Size: int64(len(body)), Mode: 0o444},
				}, nil
			})

		cli := &client{log: newTestLogger(t), api: api}

		var err error

		for i := 0; err == nil && i < 32; i++ {
			err = cli.SnapshotCopyFiles(t.Context(), modelfixture.Container(
				containerfixture.WithID(fmt.Sprintf("container-%d", i)),
				containerfixture.WithLabels(map[string]string{labels.CopyFile: "/app/config.yaml"}),
			))
		}

		require.ErrorContains(t, err, "store copy-file snapshot")
	})

	t.Run("inject missing snapshot is a no-op", func(t *testing.T) {
		t.Parallel()

		cli := &client{log: newTestLogger(t), api: mockContainer.NewMockAPIClient(t)}
		require.NoError(t, cli.injectStoredCopyFiles(t.Context(), modelfixture.Container(), "new-id"))
	})

	t.Run("inject writes then drops snapshot", func(t *testing.T) {
		t.Parallel()

		api := mockContainer.NewMockAPIClient(t)
		expectConfigCopy(t, api)
		expectDaemonInfo(api)
		api.EXPECT().
			CopyToContainer(mock.Anything, "new-id", mock.Anything).
			Return(dockerClient.CopyToContainerResult{}, nil)

		cli := &client{log: newTestLogger(t), api: api}
		source := configSource()
		require.NoError(t, cli.SnapshotCopyFiles(t.Context(), source))
		require.NoError(t, cli.injectStoredCopyFiles(t.Context(), source, "new-id"))

		_, stored := cli.copyFiles.Take(source.ID())
		assert.False(t, stored)
	})

	t.Run("inject failure still drops snapshot", func(t *testing.T) {
		t.Parallel()

		api := mockContainer.NewMockAPIClient(t)
		expectConfigCopy(t, api)
		expectDaemonInfo(api)
		api.EXPECT().
			CopyToContainer(mock.Anything, "new-id", mock.Anything).
			Return(dockerClient.CopyToContainerResult{}, errors.New("denied"))

		cli := &client{log: newTestLogger(t), api: api}
		source := configSource()
		require.NoError(t, cli.SnapshotCopyFiles(t.Context(), source))

		err := cli.injectStoredCopyFiles(t.Context(), source, "new-id")
		require.ErrorContains(t, err, "failed to copy file into new container")

		_, stored := cli.copyFiles.Take(source.ID())
		assert.False(t, stored)
	})

	t.Run("discard drops the snapshot", func(t *testing.T) {
		t.Parallel()

		api := mockContainer.NewMockAPIClient(t)
		expectConfigCopy(t, api)

		cli := &client{log: newTestLogger(t), api: api}
		source := configSource()
		require.NoError(t, cli.SnapshotCopyFiles(t.Context(), source))

		cli.DiscardCopyFiles(source.ID())

		_, stored := cli.copyFiles.Take(source.ID())
		assert.False(t, stored)
	})
}
