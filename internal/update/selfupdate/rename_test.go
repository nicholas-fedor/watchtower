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

// predecessorOf renames source to its old name through a mocked client, so a
// test can go on from a started self-update.
//
// Parameters:
//   - t: The test.
//   - source: The running Watchtower container.
//
// Returns:
//   - Predecessor: The container, under its old name.
func predecessorOf(t *testing.T, source *container.Container) Predecessor {
	t.Helper()

	renamer := mockSelfupdate.NewMockRenamer(t)
	if !container.IsOldContainer(source.Name()) {
		renamer.EXPECT().RenameContainer(mock.Anything, source, source.OldWatchtowerName()).Return(nil).Once()
	}

	predecessor, err := NewPredecessor(newTestLogger(t), t.Context(), renamer, source)
	require.NoError(t, err)

	return predecessor
}

// TestNewPredecessor verifies that a self-update renames the running Watchtower
// container to its old name and adds it to the replacement's container chain.
// A container that already has its old name is not renamed again, and a failed
// rename returns the client's error and leaves the chain alone.
func TestNewPredecessor(t *testing.T) {
	t.Parallel()

	t.Run("renames the container to its old name", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		predecessor := predecessorOf(t, source)

		assert.True(t, predecessor.HasOldName())

		chain, ok := source.GetContainerChain()
		require.True(t, ok)
		assert.Equal(t, string(source.ID()), chain)
	})

	t.Run("skips a container that already has its old name", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower-old-abc123def456")
		predecessor := predecessorOf(t, source)

		assert.True(t, predecessor.HasOldName())

		chain, ok := source.GetContainerChain()
		require.True(t, ok)
		assert.Equal(t, string(source.ID()), chain)
	})

	t.Run("returns the client's error when the rename fails", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		renameErr := errors.New("failed to rename container: daemon unavailable")

		renamer := mockSelfupdate.NewMockRenamer(t)
		renamer.EXPECT().RenameContainer(mock.Anything, source, source.OldWatchtowerName()).Return(renameErr).Once()

		predecessor, err := NewPredecessor(newTestLogger(t), t.Context(), renamer, source)
		require.Same(t, renameErr, err)
		assert.False(t, predecessor.HasOldName())

		_, ok := source.GetContainerChain()
		assert.False(t, ok)
	})
}

// newTemporaryRename renames source to its temporary name through a mocked
// client, so a test can go on from a started rename.
//
// Parameters:
//   - t: The test.
//   - source: The container to replace.
//
// Returns:
//   - TemporaryRename: The started rename.
func newTemporaryRename(t *testing.T, source *container.Container) TemporaryRename {
	t.Helper()

	renamer := mockSelfupdate.NewMockRenamer(t)
	renamer.EXPECT().RenameContainer(mock.Anything, source, source.ReplacedName()).Return(nil).Once()

	rename, err := RenameForReplacement(t.Context(), renamer, source, source.ReplacedName())
	require.NoError(t, err)

	return rename
}

// TestRenameForReplacement verifies that a container is renamed to its
// temporary name and that a failed rename returns the client's error without
// renaming anything.
func TestRenameForReplacement(t *testing.T) {
	t.Parallel()

	t.Run("renames the container", func(t *testing.T) {
		t.Parallel()

		rename := newTemporaryRename(t, newRenameSource("/app"))
		assert.True(t, rename.HasTemporaryName())
	})

	t.Run("returns the client's error when the rename fails", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/app")
		renameErr := errors.New("failed to rename container: daemon unavailable")

		renamer := mockSelfupdate.NewMockRenamer(t)
		renamer.EXPECT().RenameContainer(mock.Anything, source, source.ReplacedName()).Return(renameErr).Once()

		rename, err := RenameForReplacement(t.Context(), renamer, source, source.ReplacedName())
		require.Same(t, renameErr, err)
		assert.False(t, rename.HasTemporaryName())
	})
}

// TestPredecessor_Abort verifies that a self-update whose replacement fails gives
// the running container its original name back and returns the cause.
//
// A failed rename back is added to the returned error with both names, since
// the update reports that error once with the container. A container that
// already had its old name keeps it. A restart that renamed nothing returns the
// cause alone.
func TestPredecessor_Abort(t *testing.T) {
	t.Parallel()

	t.Run("restores the name after a failed create", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		predecessor := predecessorOf(t, source)
		cause := errors.New("failed to create container: daemon unavailable")

		client := mockSelfupdate.NewMockPredecessorClient(t)
		client.EXPECT().RenameContainer(mock.Anything, source, "watchtower").Return(nil).Once()

		err := predecessor.Abort(t.Context(), client, "", updateParams.Params{}, cause)
		require.Same(t, cause, err)
		assert.False(t, predecessor.HasOldName())
	})

	t.Run("keeps an old name it did not give", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower-old-abc123def456")
		predecessor := predecessorOf(t, source)
		cause := errors.New("failed to start container replacement-id: daemon unavailable")
		failed := mockUpdate.CreateMockContainer(string(renameReplacementID), "/watchtower-old-abc123def456", "watchtower:latest", time.Now())

		// The mock fails the test on any rename.
		client := mockSelfupdate.NewMockPredecessorClient(t)
		client.EXPECT().GetContainer(mock.Anything, renameReplacementID).Return(failed, nil).Once()
		client.EXPECT().StopAndRemoveContainer(mock.Anything, failed, time.Duration(0)).Return(nil).Once()

		err := predecessor.Abort(t.Context(), client, renameReplacementID, updateParams.Params{}, cause)
		require.Same(t, cause, err)
		assert.True(t, predecessor.HasOldName())
	})

	t.Run("returns the cause when nothing was renamed", func(t *testing.T) {
		t.Parallel()

		var predecessor Predecessor

		cause := errors.New("failed to start container replacement-id: daemon unavailable")

		err := predecessor.Abort(t.Context(), mockSelfupdate.NewMockPredecessorClient(t), renameReplacementID, updateParams.Params{}, cause)
		require.Same(t, cause, err)
		assert.False(t, predecessor.HasOldName())
	})

	t.Run("adds a failed rename back to the error", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		predecessor := predecessorOf(t, source)
		cause := errors.New("failed to create container: daemon unavailable")

		client := mockSelfupdate.NewMockPredecessorClient(t)
		client.EXPECT().RenameContainer(mock.Anything, source, "watchtower").
			Return(errors.New("failed to rename container: daemon unavailable")).Once()

		err := predecessor.Abort(t.Context(), client, "", updateParams.Params{}, cause)
		require.ErrorIs(t, err, cause)
		require.ErrorIs(t, err, errNameNotRestored)
		require.EqualError(t, err, cause.Error()+
			", and could not restore Watchtower's container name watchtower from "+source.OldWatchtowerName()+
			": failed to rename container: daemon unavailable")
		assert.True(t, predecessor.HasOldName(), "the source still has its old name")
	})
}

// TestPredecessor_Retire verifies that the container under its old name gets the
// restart policy "no" once its replacement runs, and that a restart that
// renamed nothing changes no restart policy.
func TestPredecessor_Retire(t *testing.T) {
	t.Parallel()

	t.Run("disables the restarts of the renamed container", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/watchtower")
		predecessor := predecessorOf(t, source)

		client := mockSelfupdate.NewMockRestartPolicySetter(t)
		client.EXPECT().SetRestartPolicy(mock.Anything, source.ID(),
			dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled}).Return(nil).Once()

		predecessor.Retire(t.Context(), client)
	})

	t.Run("does nothing when nothing was renamed", func(t *testing.T) {
		t.Parallel()

		var predecessor Predecessor

		// The mock fails the test on any call.
		predecessor.Retire(t.Context(), mockSelfupdate.NewMockRestartPolicySetter(t))
	})
}

// TestTemporaryRename_Rollback verifies that a failed replacement is removed
// before the container takes its name back.
//
// The rename back runs detached from a canceled update, with the stop timeout
// plus 30 seconds. A failed rename back returns the client's error and leaves
// the container under its temporary name. A rename of nothing does nothing.
func TestTemporaryRename_Rollback(t *testing.T) {
	t.Parallel()

	t.Run("restores the name when no replacement was created", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/app")
		rename := newTemporaryRename(t, source)

		client := mockSelfupdate.NewMockRollbackClient(t)
		client.EXPECT().RenameContainer(mock.Anything, source, "app").Return(nil).Once()

		require.NoError(t, rename.Rollback(newTestLogger(t), t.Context(), client, "", updateParams.Params{}))
		assert.False(t, rename.HasTemporaryName())
	})

	t.Run("removes the replacement before restoring the name", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/app")
		rename := newTemporaryRename(t, source)
		failed := mockUpdate.CreateMockContainer(string(renameReplacementID), "/app", "app:latest", time.Now())

		client := mockSelfupdate.NewMockRollbackClient(t)
		removal := client.EXPECT().GetContainer(mock.Anything, renameReplacementID).Return(failed, nil).Once()
		stop := client.EXPECT().StopAndRemoveContainer(mock.Anything, failed, time.Second).Return(nil).Once().
			NotBefore(removal)
		client.EXPECT().RenameContainer(mock.Anything, source, "app").Return(nil).Once().NotBefore(stop)

		err := rename.Rollback(newTestLogger(t), t.Context(), client, renameReplacementID,
			updateParams.Params{Timeout: time.Second})
		require.NoError(t, err)
		assert.False(t, rename.HasTemporaryName())
	})

	t.Run("does nothing when nothing was renamed", func(t *testing.T) {
		t.Parallel()

		var rename TemporaryRename

		// The mock fails the test on any call.
		err := rename.Rollback(newTestLogger(t), t.Context(), mockSelfupdate.NewMockRollbackClient(t),
			renameReplacementID, updateParams.Params{})
		require.NoError(t, err)
		assert.False(t, rename.HasTemporaryName())
	})

	t.Run("detaches the rename back from a canceled update", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			source := newRenameSource("/app")
			rename := newTemporaryRename(t, source)

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			// The bubble's clock stands still while code runs, so the
			// deadline matches exactly.
			client := mockSelfupdate.NewMockRollbackClient(t)
			client.EXPECT().RenameContainer(mock.MatchedBy(func(ctx context.Context) bool {
				deadline, hasDeadline := ctx.Deadline()

				return ctx.Err() == nil && hasDeadline && deadline.Equal(time.Now().Add(40*time.Second))
			}), source, "app").Return(nil).Once()

			err := rename.Rollback(newTestLogger(t), ctx, client, "", updateParams.Params{Timeout: 10 * time.Second})
			require.NoError(t, err)
		})
	})

	t.Run("returns the error of a failed rename back", func(t *testing.T) {
		t.Parallel()

		source := newRenameSource("/app")
		rename := newTemporaryRename(t, source)
		renameErr := errors.New("failed to rename container: daemon unavailable")

		client := mockSelfupdate.NewMockRollbackClient(t)
		client.EXPECT().RenameContainer(mock.Anything, source, "app").Return(renameErr).Once()

		err := rename.Rollback(newTestLogger(t), t.Context(), client, "", updateParams.Params{})
		require.Same(t, renameErr, err)
		assert.True(t, rename.HasTemporaryName(), "the container is still under its temporary name")
	})
}

// TestRemoveFailedReplacement verifies the bound for removing a replacement
// that failed to start. The bound is the stop timeout plus 30 seconds. When the
// container the replacement was created from sets a longer stop timeout, the
// stop waits that long, so the bound is that stop timeout plus 30 seconds. A
// stop timeout too long for a duration saturates the bound instead of
// overflowing it. An empty ID removes nothing, and a replacement already gone
// is not a failure.
func TestRemoveFailedReplacement(t *testing.T) {
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
