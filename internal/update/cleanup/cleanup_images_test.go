package cleanup

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// cleanupTestWatchtower returns a running Watchtower container with the given
// ID, name, and image. Its image ID is the image name.
func cleanupTestWatchtower(id, name, image string) types.Container {
	return mockUpdate.CreateMockContainerWithConfig(
		id,
		"/"+name,
		image,
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image:  image,
			Labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
		},
	)
}

// imageEntries returns the image and container name of each entry.
func imageEntries(infos []types.RemovedImageInfo) []string {
	entries := make([]string, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, string(info.ImageID)+" "+info.ContainerName)
	}

	return entries
}

// removedImageIDs returns the image ID of each entry.
func removedImageIDs(infos []types.RemovedImageInfo) []types.ImageID {
	ids := make([]types.ImageID, 0, len(infos))
	for _, info := range infos {
		ids = append(ids, info.ImageID)
	}

	return ids
}

// TestRemoveExcessContainers_CallerListCollectsImages verifies that when the
// caller passes an image list, the images of removed instances are added to
// it once per container, without removing them and without touching the
// caller's earlier entries, so the caller removes each image once.
func TestRemoveExcessContainers_CallerListCollectsImages(t *testing.T) {
	t.Parallel()

	current := cleanupTestWatchtower("c000000000000000000000000000000000000000000000000000000000000000",
		"watchtower", "org/watchtower:2")
	old1 := cleanupTestWatchtower("a000000000000000000000000000000000000000000000000000000000000000",
		"watchtower-old-a", "org/watchtower:1")
	old2 := cleanupTestWatchtower("b000000000000000000000000000000000000000000000000000000000000000",
		"watchtower-old-b", "org/watchtower:1")

	// The mock fails the test on any image removal.
	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().StopAndRemoveContainer(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)

	images := []types.RemovedImageInfo{
		{ImageID: "org/app:1", ContainerName: "app"},
		{ImageID: "org/watchtower:1", ContainerName: "watchtower-old-a"},
	}

	removed, err := removeExcessContainers(newTestLogger(t), t.Context(), client,
		[]types.Container{old1, old2}, true, current, &images)
	require.NoError(t, err)
	assert.Equal(t, 2, removed)

	assert.Equal(t, []string{
		"org/app:1 app",
		"org/watchtower:1 watchtower-old-a",
		"org/watchtower:1 watchtower-old-b",
	}, imageEntries(images))
}

// TestRemoveExcessContainers_PartialFailureKeepsCallerList verifies that when
// an instance cannot be stopped, no image of this cleanup is collected, and
// the caller's earlier entries stay on the list. The retry delays pass on the
// synctest clock.
func TestRemoveExcessContainers_PartialFailureKeepsCallerList(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		current := cleanupTestWatchtower("c000000000000000000000000000000000000000000000000000000000000000",
			"watchtower", "org/watchtower:2")
		stopped := cleanupTestWatchtower("a000000000000000000000000000000000000000000000000000000000000000",
			"watchtower-old-a", "org/watchtower:1")
		stuck := cleanupTestWatchtower("b000000000000000000000000000000000000000000000000000000000000000",
			"watchtower-old-b", "org/watchtower:1")

		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().StopAndRemoveContainer(mock.Anything, stopped, mock.Anything).Return(nil).Once()
		client.EXPECT().StopAndRemoveContainer(mock.Anything, stuck, mock.Anything).
			Return(errors.New("daemon busy")).Times(maxRemovalAttempts)

		images := []types.RemovedImageInfo{{ImageID: "org/app:1", ContainerName: "app"}}

		removed, err := removeExcessContainers(newTestLogger(t), t.Context(), client,
			[]types.Container{stopped, stuck}, true, current, &images)
		require.ErrorIs(t, err, errStopWatchtowerFailed)
		assert.Equal(t, 1, removed)

		assert.Equal(t, []string{"org/app:1 app"}, imageEntries(images))
	})
}

// TestRemoveExcessContainers_CancelKeepsRemovedInstanceImages verifies that
// when the cleanup is canceled while retrying an instance, the images of the
// instances already removed stay on the caller's list, except images that an
// instance left in place still uses.
func TestRemoveExcessContainers_CancelKeepsRemovedInstanceImages(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		current := cleanupTestWatchtower("c000000000000000000000000000000000000000000000000000000000000000",
			"watchtower", "org/watchtower:5")
		removedShared := cleanupTestWatchtower("a000000000000000000000000000000000000000000000000000000000000000",
			"watchtower-old-a", "org/watchtower:1")
		removedOwn := cleanupTestWatchtower("b000000000000000000000000000000000000000000000000000000000000000",
			"watchtower-old-b", "org/watchtower:3")
		stuck := cleanupTestWatchtower("d000000000000000000000000000000000000000000000000000000000000000",
			"watchtower-old-d", "org/watchtower:4")
		pending := cleanupTestWatchtower("e000000000000000000000000000000000000000000000000000000000000000",
			"watchtower-old-e", "org/watchtower:1")

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// The mock fails the test on any image removal.
		client := mockContainerClient.NewMockClient(t)
		client.EXPECT().StopAndRemoveContainer(mock.Anything, removedShared, mock.Anything).Return(nil).Once()
		client.EXPECT().StopAndRemoveContainer(mock.Anything, removedOwn, mock.Anything).Return(nil).Once()
		client.EXPECT().StopAndRemoveContainer(mock.Anything, stuck, mock.Anything).
			RunAndReturn(func(context.Context, types.Container, time.Duration) error {
				cancel()

				return errors.New("daemon busy")
			}).Once()

		images := []types.RemovedImageInfo{{ImageID: "org/app:1", ContainerName: "app"}}

		removed, err := removeExcessContainers(newTestLogger(t), ctx, client,
			[]types.Container{removedShared, removedOwn, stuck, pending}, true, current, &images)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 2, removed)

		// The pending instance still uses org/watchtower:1.
		assert.Equal(t, []string{
			"org/app:1 app",
			"org/watchtower:3 watchtower-old-b",
		}, imageEntries(images))
	})
}

// TestRemoveExcessContainers_NoCallerListRemovesImages verifies that without
// a caller list, the images of removed instances are removed right away, once
// per image.
func TestRemoveExcessContainers_NoCallerListRemovesImages(t *testing.T) {
	t.Parallel()

	current := cleanupTestWatchtower("c000000000000000000000000000000000000000000000000000000000000000",
		"watchtower", "org/watchtower:2")
	old1 := cleanupTestWatchtower("a000000000000000000000000000000000000000000000000000000000000000",
		"watchtower-old-a", "org/watchtower:1")
	old2 := cleanupTestWatchtower("b000000000000000000000000000000000000000000000000000000000000000",
		"watchtower-old-b", "org/watchtower:1")

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().StopAndRemoveContainer(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	client.EXPECT().RemoveImageByID(mock.Anything, types.ImageID("org/watchtower:1"), mock.Anything).
		Return(nil).Once()

	removed, err := removeExcessContainers(newTestLogger(t), t.Context(), client,
		[]types.Container{old1, old2}, true, current, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, removed)
}
