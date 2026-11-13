package selfupdate

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	mockSelfupdate "github.com/nicholas-fedor/watchtower/internal/update/selfupdate/mocks"
)

// renameReplacementID is the ID of the replacement a rename-based self-update creates.
const renameReplacementID container.ContainerID = "replacement-id"

// newRenameSource returns a running Watchtower container with the given name,
// as a rename-based self-update finds it.
//
// Parameters:
//   - name: The container name, with a leading slash.
//
// Returns:
//   - *container.Container: A Watchtower container with the ID "abc123def456".
func newRenameSource(name string) *container.Container {
	return mockUpdate.CreateMockContainerWithConfig(
		"abc123def456",
		name,
		"watchtower:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{Labels: map[string]string{"com.centurylinklabs.watchtower": "true"}},
	)
}

// beginRename renames source to its old name through a mocked client, so a
// test can go on from a started self-update.
//
// Parameters:
//   - t: The test.
//   - source: The running Watchtower container.
//
// Returns:
//   - Rename: The started self-update.
func beginRename(t *testing.T, source *container.Container) Rename {
	t.Helper()

	renamer := mockSelfupdate.NewMockRenamer(t)
	if !container.IsOldContainer(source.Name()) {
		renamer.EXPECT().RenameContainer(mock.Anything, source, source.OldName()).Return(nil).Once()
	}

	rename, err := BeginRename(newTestLogger(t), t.Context(), renamer, source)
	require.NoError(t, err)

	return rename
}

// TestBeginRename verifies that a self-update renames the running Watchtower
// container to its old name and adds it to the replacement's container chain.
// A container that already has its old name is not renamed again, and a failed
// rename returns the client's error and leaves the chain alone.
func TestBeginRename(t *testing.T) {
	t.Parallel()

	t.Run("renames the container to its old name", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		rename := beginRename(t, source)

		assert.True(t, rename.Renamed())

		chain, ok := source.GetContainerChain()
		require.True(t, ok)
		assert.Equal(t, string(source.ID()), chain)
	})

	t.Run("skips a container that already has its old name", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower-old-abc123def456")
		rename := beginRename(t, source)

		assert.True(t, rename.Renamed())

		chain, ok := source.GetContainerChain()
		require.True(t, ok)
		assert.Equal(t, string(source.ID()), chain)
	})

	t.Run("returns the client's error when the rename fails", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		renameErr := errors.New("failed to rename container: daemon unavailable")

		renamer := mockSelfupdate.NewMockRenamer(t)
		renamer.EXPECT().RenameContainer(mock.Anything, source, source.OldName()).Return(renameErr).Once()

		rename, err := BeginRename(newTestLogger(t), t.Context(), renamer, source)
		require.Same(t, renameErr, err)
		assert.False(t, rename.Renamed())

		_, ok := source.GetContainerChain()
		assert.False(t, ok)
	})
}

// TestRename_Abort verifies that a self-update whose replacement fails gives
// the running container its original name back.
//
// A created replacement holds the original name, so it is removed before the
// rename back. A failed rename back is added to the returned error with both
// names, since the update reports that error once with the container. A
// container that already had its old name keeps it, and a restart that renamed
// nothing returns the cause alone.
func TestRename_Abort(t *testing.T) {
	t.Parallel()

	t.Run("restores the name after a failed create", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		rename := beginRename(t, source)
		cause := errors.New("failed to create container: daemon unavailable")

		client := mockSelfupdate.NewMockRenameClient(t)
		client.EXPECT().RenameContainer(mock.Anything, source, "watchtower").Return(nil).Once()

		err := rename.Abort(t.Context(), client, "", updateParams.Params{}, cause)
		require.Same(t, cause, err)
		assert.False(t, rename.Renamed())
	})

	t.Run("removes a failed replacement before restoring the name", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		rename := beginRename(t, source)
		cause := errors.New("failed to start container replacement-id: daemon unavailable")
		failed := mockUpdate.CreateMockContainer(string(renameReplacementID), "/watchtower", "watchtower:latest", time.Now())

		client := mockSelfupdate.NewMockRenameClient(t)
		removal := client.EXPECT().GetContainer(mock.Anything, renameReplacementID).Return(failed, nil).Once()
		stop := client.EXPECT().StopAndRemoveContainer(mock.Anything, failed, time.Second).Return(nil).Once().
			NotBefore(removal)
		client.EXPECT().RenameContainer(mock.Anything, source, "watchtower").Return(nil).Once().NotBefore(stop)

		err := rename.Abort(t.Context(), client, renameReplacementID, updateParams.Params{Timeout: time.Second}, cause)
		require.Same(t, cause, err)
		assert.False(t, rename.Renamed())
	})

	t.Run("keeps an old name it did not give", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower-old-abc123def456")
		rename := beginRename(t, source)
		cause := errors.New("failed to start container replacement-id: daemon unavailable")
		failed := mockUpdate.CreateMockContainer(string(renameReplacementID), "/watchtower-old-abc123def456", "watchtower:latest", time.Now())

		// The mock fails the test on any rename.
		client := mockSelfupdate.NewMockRenameClient(t)
		client.EXPECT().GetContainer(mock.Anything, renameReplacementID).Return(failed, nil).Once()
		client.EXPECT().StopAndRemoveContainer(mock.Anything, failed, time.Duration(0)).Return(nil).Once()

		err := rename.Abort(t.Context(), client, renameReplacementID, updateParams.Params{}, cause)
		require.Same(t, cause, err)
		assert.True(t, rename.Renamed())
	})

	t.Run("returns the cause when nothing was renamed", func(t *testing.T) {
		t.Parallel()

		var rename Rename

		cause := errors.New("failed to start container replacement-id: daemon unavailable")

		err := rename.Abort(t.Context(), mockSelfupdate.NewMockRenameClient(t), renameReplacementID, updateParams.Params{}, cause)
		require.Same(t, cause, err)
		assert.False(t, rename.Renamed())
	})

	t.Run("detaches the rename back from a canceled self-update", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			source := newRenameSource("/watchtower")
			rename := beginRename(t, source)

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			// The rename back gets the stop timeout plus 30 seconds. The
			// bubble's clock stands still while code runs, so the deadline
			// matches exactly.
			client := mockSelfupdate.NewMockRenameClient(t)
			client.EXPECT().RenameContainer(mock.MatchedBy(func(ctx context.Context) bool {
				deadline, hasDeadline := ctx.Deadline()

				return ctx.Err() == nil && hasDeadline && deadline.Equal(time.Now().Add(40*time.Second))
			}), source, "watchtower").Return(nil).Once()

			err := rename.Abort(ctx, client, "", updateParams.Params{Timeout: 10 * time.Second}, context.Canceled)
			require.ErrorIs(t, err, context.Canceled)
		})
	})

	t.Run("adds a failed rename back to the error", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		rename := beginRename(t, source)
		cause := errors.New("failed to create container: daemon unavailable")

		client := mockSelfupdate.NewMockRenameClient(t)
		client.EXPECT().RenameContainer(mock.Anything, source, "watchtower").
			Return(errors.New("failed to rename container: daemon unavailable")).Once()

		err := rename.Abort(t.Context(), client, "", updateParams.Params{}, cause)
		require.ErrorIs(t, err, cause)
		require.ErrorIs(t, err, errNameNotRestored)
		require.EqualError(t, err, cause.Error()+
			", and could not restore Watchtower's container name watchtower from "+source.OldName()+
			": failed to rename container: daemon unavailable")
		assert.True(t, rename.Renamed(), "the source still has its old name")
	})
}

// TestRename_Retire verifies that the container under its old name gets the
// restart policy "no" once its replacement runs, and that a restart that
// renamed nothing changes no restart policy.
func TestRename_Retire(t *testing.T) {
	t.Parallel()

	t.Run("disables the restarts of the renamed container", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		rename := beginRename(t, source)

		client := mockSelfupdate.NewMockRestartPolicySetter(t)
		client.EXPECT().SetRestartPolicy(mock.Anything, source.ID(),
			dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled}).Return(nil).Once()

		rename.Retire(t.Context(), client)
	})

	t.Run("does nothing when nothing was renamed", func(t *testing.T) {
		t.Parallel()

		var rename Rename

		// The mock fails the test on any call.
		rename.Retire(t.Context(), mockSelfupdate.NewMockRestartPolicySetter(t))
	})
}

// Test_removeFailedReplacement verifies the bound for removing a replacement
// that failed to start. The bound is the stop timeout plus 30 seconds. When the
// container the replacement was created from sets a longer stop timeout, the
// stop waits that long, so the bound is that stop timeout plus 30 seconds. A
// stop timeout too long for a duration saturates the bound instead of
// overflowing it. An empty ID removes nothing, and a replacement already gone
// is not a failure.
func Test_removeFailedReplacement(t *testing.T) {
	t.Parallel()

	// largestBound is the bound for a container stop timeout of the largest int.
	// A 64-bit int of seconds is too long for a duration, so the bound
	// saturates. A 32-bit int of seconds fits, and the bound adds 30 seconds.
	largestBound := time.Duration(math.MaxInt64)
	if strconv.IntSize == 32 {
		largestBound = math.MaxInt32*time.Second + 30*time.Second
	}

	tests := []struct {
		name        string
		stopTimeout *int
		removeErr   error
		want        time.Duration
	}{
		{name: "no container stop timeout", stopTimeout: nil, want: time.Minute},
		{name: "shorter container stop timeout", stopTimeout: new(10), want: time.Minute},
		{name: "longer container stop timeout", stopTimeout: new(120), want: 150 * time.Second},
		{name: "largest container stop timeout", stopTimeout: new(math.MaxInt), want: largestBound},
		{name: "replacement already gone", stopTimeout: nil, removeErr: cerrdefs.ErrNotFound, want: time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				source := mockUpdate.CreateMockContainerWithConfig(
					"wt-old-id",
					"/watchtower-old-wt-old-id",
					"watchtower:v1",
					true,
					false,
					time.Now(),
					&dockerContainer.Config{Image: "watchtower:v1", StopTimeout: tt.stopTimeout},
				)
				failedNew := mockUpdate.CreateMockContainer(string(renameReplacementID), "/watchtower", "watchtower:v2", time.Now())

				// boundCtx matches a context whose deadline is the expected bound
				// from now. The bubble's clock stands still while code runs, so
				// the deadline matches exactly.
				boundCtx := mock.MatchedBy(func(ctx context.Context) bool {
					deadline, hasDeadline := ctx.Deadline()

					return hasDeadline && deadline.Equal(time.Now().Add(tt.want))
				})

				client := mockSelfupdate.NewMockReplacementRemover(t)
				client.EXPECT().GetContainer(boundCtx, renameReplacementID).Return(failedNew, nil).Once()
				client.EXPECT().StopAndRemoveContainer(boundCtx, failedNew, 30*time.Second).Return(tt.removeErr).Once()

				removeFailedReplacement(newTestLogger(t), t.Context(), client, source, renameReplacementID,
					updateParams.Params{Timeout: 30 * time.Second})
			})
		})
	}

	t.Run("an empty ID removes nothing", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")

		// The mock fails the test on any call.
		removeFailedReplacement(newTestLogger(t), t.Context(), mockSelfupdate.NewMockReplacementRemover(t),
			source, "", updateParams.Params{})
	})
}
