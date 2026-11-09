package restart

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/nicholas-fedor/watchtower/internal/container"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	mockRestart "github.com/nicholas-fedor/watchtower/internal/update/restart/mocks"
)

// staleMockContainer returns a stale container for the copy-file recreate tests.
//
// Returns:
//   - *container.Container: The stale container.
func staleMockContainer() *container.Container {
	source := mockUpdate.CreateMockContainer("container-id", "/app", "app:latest", time.Now())
	source.SetStale(true)

	return source
}

// TestStopStaleContainerCopyFile verifies that a stale container's labeled
// files are snapshotted before it stops and kept on success, that a failed
// snapshot prevents the stop and is reported as such, and that the snapshot is
// discarded when the stop fails or the container is already gone.
func TestStopStaleContainerCopyFile(t *testing.T) {
	t.Parallel()

	t.Run("snapshots before stop and does not discard on success", func(t *testing.T) {
		t.Parallel()

		source := staleMockContainer()
		client := mockRestart.NewMockClient(t)
		client.EXPECT().
			SnapshotCopyFiles(mock.Anything, source).
			Return(nil)
		client.EXPECT().
			StopAndRemoveContainer(mock.Anything, source, time.Second).
			Return(nil)

		stopped, err := stopStaleContainer(
			newTestLogger(t),
			t.Context(),
			source,
			client,
			updateParams.Params{Timeout: time.Second},
		)
		require.NoError(t, err)
		assert.True(t, stopped)
	})

	t.Run("does not stop when snapshot fails", func(t *testing.T) {
		t.Parallel()

		source := staleMockContainer()
		client := mockRestart.NewMockClient(t)
		snapshotErr := errors.New("denied")

		client.EXPECT().
			SnapshotCopyFiles(mock.Anything, source).
			Return(snapshotErr)

		_, err := stopStaleContainer(
			newTestLogger(t),
			t.Context(),
			source,
			client,
			updateParams.Params{Timeout: time.Second},
		)
		require.ErrorIs(t, err, snapshotErr)
		assert.ErrorContains(t, err, "snapshot copy-file paths")
	})

	t.Run("discards the snapshot when stop fails", func(t *testing.T) {
		t.Parallel()

		source := staleMockContainer()
		client := mockRestart.NewMockClient(t)
		client.EXPECT().
			SnapshotCopyFiles(mock.Anything, source).
			Return(nil)
		client.EXPECT().
			StopAndRemoveContainer(mock.Anything, source, time.Second).
			Return(errors.New("busy"))
		client.EXPECT().DiscardCopyFiles(source.ID())

		_, err := stopStaleContainer(
			newTestLogger(t),
			t.Context(),
			source,
			client,
			updateParams.Params{Timeout: time.Second},
		)
		require.Error(t, err)
	})

	t.Run("skips and discards the snapshot when the container is already gone", func(t *testing.T) {
		t.Parallel()

		source := staleMockContainer()
		client := mockRestart.NewMockClient(t)
		client.EXPECT().
			SnapshotCopyFiles(mock.Anything, source).
			Return(nil)
		client.EXPECT().
			StopAndRemoveContainer(mock.Anything, source, time.Second).
			Return(cerrdefs.ErrNotFound)
		client.EXPECT().DiscardCopyFiles(source.ID())

		stopped, err := stopStaleContainer(
			newTestLogger(t),
			t.Context(),
			source,
			client,
			updateParams.Params{Timeout: time.Second},
		)
		assert.False(t, stopped)
		assert.True(t, isSkip(err))
		assert.ErrorIs(t, err, errContainerGone)
	})
}
