package update

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	"github.com/nicholas-fedor/watchtower/internal/registry/ratelimit"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
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

// removedContainerNames returns the container name of each entry.
func removedContainerNames(infos []types.RemovedImageInfo) []string {
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.ContainerName)
	}

	return names
}

// TestUpdateAction_Cleanup verifies that an update with cleanup enabled
// collects one image entry per updated container for the caller to remove,
// without removing any image itself.
func TestUpdateAction_Cleanup(t *testing.T) {
	t.Parallel()

	t.Run("watchtower has been instructed to clean up", func(t *testing.T) {
		t.Parallel()

		t.Run("there are multiple containers using the same image", func(t *testing.T) {
			t.Parallel()

			t.Run("should collect one entry per container for deferred cleanup", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateMockClient(getCommonTestData(), false, false)
				client.TestData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
					"test-container-03": true,
				}
				report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, types.UpdateParams{Cleanup: true}, ratelimit.New())

				require.NoError(t, err)
				require.Len(t, report.Updated(), 3)
				// Each container using the same image gets its own entry so that
				// split-by-container notifications report correctly. Image-level
				// deduplication happens later in performImageCleanup.
				require.Len(t, cleanupImageInfos, 3)
				assert.Contains(t, removedImageIDs(cleanupImageInfos), types.ImageID("fake-image:latest"))
				assert.Contains(t, removedContainerNames(cleanupImageInfos), "test-container-01")
				assert.Contains(t, removedContainerNames(cleanupImageInfos), "test-container-02")
				assert.Contains(t, removedContainerNames(cleanupImageInfos), "test-container-03")
				assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
			})
		})

		t.Run("there are multiple containers using different images", func(t *testing.T) {
			t.Parallel()

			t.Run("should collect one entry per container for each image", func(t *testing.T) {
				t.Parallel()

				testData := getCommonTestData()
				testData.Containers = append(
					testData.Containers,
					mockUpdate.CreateMockContainer(
						"unique-test-container",
						"unique-test-container",
						"unique-fake-image:latest",
						time.Now(),
					),
				)
				client := mockUpdate.CreateMockClient(testData, false, false)
				client.TestData.Staleness = map[string]bool{
					"test-container-01":     true,
					"test-container-02":     true,
					"test-container-03":     true,
					"unique-test-container": true,
				}
				report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, types.UpdateParams{Cleanup: true}, ratelimit.New())

				require.NoError(t, err)
				require.Len(t, report.Updated(), 4)
				assert.Contains(t, removedImageIDs(cleanupImageInfos), types.ImageID("fake-image:latest"))
				assert.Contains(t, removedImageIDs(cleanupImageInfos), types.ImageID("unique-fake-image:latest"))
				// 3 containers for fake-image:latest + 1 container for unique-fake-image:latest
				require.Len(t, cleanupImageInfos, 4)
				assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
			})
		})

		t.Run("there are linked containers being updated", func(t *testing.T) {
			t.Parallel()

			t.Run("should collect only the stale container's image ID", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateMockClient(getLinkedTestData(true), false, false)
				client.TestData.Staleness["test-container-01"] = true
				report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, types.UpdateParams{Cleanup: true}, ratelimit.New())

				require.NoError(t, err)
				require.Len(t, report.Updated(), 1)
				assert.Contains(t, removedImageIDs(cleanupImageInfos), types.ImageID("fake-image1:latest"))
				require.Len(t, cleanupImageInfos, 1)
				assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
			})
		})

		t.Run("performing a rolling restart update", func(t *testing.T) {
			t.Parallel()

			t.Run("should collect one entry per container for deferred cleanup", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateMockClient(getCommonTestData(), false, false)
				client.TestData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
					"test-container-03": true,
				}
				report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, types.UpdateParams{Cleanup: true, RollingRestart: true}, ratelimit.New())

				require.NoError(t, err)
				require.Len(t, report.Updated(), 3)
				assert.Contains(t, removedImageIDs(cleanupImageInfos), types.ImageID("fake-image:latest"))
				// Each container using the same image gets its own entry for split notifications.
				require.Len(t, cleanupImageInfos, 3)
				assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
				assert.Equal(t, int32(3), client.TestData.WaitForContainerHealthyCount.Load(), "WaitForContainerHealthy should be called for each updated container")
			})
		})

		t.Run("updating a linked container with missing image info", func(t *testing.T) {
			t.Parallel()

			t.Run("should update the stale container and collect its image entry", func(t *testing.T) {
				t.Parallel()

				client := mockUpdate.CreateMockClient(getLinkedTestData(false), false, false)
				client.TestData.Staleness["test-container-01"] = true
				report, cleanupImageInfos, err := update(newTestLogger(t), t.Context(), client, types.UpdateParams{Cleanup: true}, ratelimit.New())

				require.NoError(t, err)
				require.Len(t, report.Updated(), 1)
				require.Len(t, report.Fresh(), 1)
				assert.Contains(t, removedImageIDs(cleanupImageInfos), types.ImageID("fake-image1:latest"))
				require.Len(t, cleanupImageInfos, 1)
				assert.Equal(t, int32(0), client.TestData.TriedToRemoveImageCount.Load(), "RemoveImageByID should not be called during Update")
			})
		})
	})
}

// TestUpdate_OldInstanceImageJoinsSessionCleanup verifies that a scan removes
// an old Watchtower instance left from a self-update and returns its image on
// the session's cleanup list, without removing the image during the scan, so
// the session removes and reports it once with the other images.
func TestUpdate_OldInstanceImageJoinsSessionCleanup(t *testing.T) {
	t.Parallel()

	const currentID = "c000000000000000000000000000000000000000000000000000000000000000"

	current := cleanupTestWatchtower(currentID, "watchtower", "org/watchtower:2")
	old := cleanupTestWatchtower("a000000000000000000000000000000000000000000000000000000000000000",
		"watchtower-old-a", "org/watchtower:1")

	// The mock fails the test on any image removal.
	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{current, old}, nil)
	client.EXPECT().StopAndRemoveContainer(mock.Anything, old, mock.Anything).Return(nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c types.Container, _ types.UpdateParams) (bool, types.ImageID, string, error) {
			return false, c.ImageID(), "", nil
		}).Maybe()

	_, cleanup, err := update(newTestLogger(t), t.Context(), client,
		types.UpdateParams{Cleanup: true, CurrentContainerID: currentID}, ratelimit.New())
	require.NoError(t, err)

	assert.Equal(t, []string{"org/watchtower:1 watchtower-old-a"}, imageEntries(cleanup))
}

// TestUpdate_SortFailureKeepsOldInstanceImage verifies that when the
// containers cannot be ordered, the scan still returns the image of an old
// Watchtower instance it already removed, so the session removes it.
func TestUpdate_SortFailureKeepsOldInstanceImage(t *testing.T) {
	t.Parallel()

	const currentID = "c000000000000000000000000000000000000000000000000000000000000000"

	current := cleanupTestWatchtower(currentID, "watchtower", "org/watchtower:2")
	old := cleanupTestWatchtower("a000000000000000000000000000000000000000000000000000000000000000",
		"watchtower-old-a", "org/watchtower:1")

	// Two containers with one name make the dependency graph fail.
	app1 := mockUpdate.CreateMockContainerWithConfig(
		"d000000000000000000000000000000000000000000000000000000000000000",
		"/app", "org/app:1", true, false, time.Now(),
		&dockerContainer.Config{Image: "org/app:1", Labels: map[string]string{}},
	)
	app2 := mockUpdate.CreateMockContainerWithConfig(
		"e000000000000000000000000000000000000000000000000000000000000000",
		"/app", "org/app:1", true, false, time.Now(),
		&dockerContainer.Config{Image: "org/app:1", Labels: map[string]string{}},
	)

	// The mock fails the test on any image removal.
	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).
		Return([]types.Container{current, old, app1, app2}, nil)
	client.EXPECT().StopAndRemoveContainer(mock.Anything, old, mock.Anything).Return(nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c types.Container, _ types.UpdateParams) (bool, types.ImageID, string, error) {
			return false, c.ImageID(), "", nil
		}).Maybe()

	_, cleanup, err := update(newTestLogger(t), t.Context(), client,
		types.UpdateParams{Cleanup: true, CurrentContainerID: currentID}, ratelimit.New())
	require.ErrorIs(t, err, errSortDependenciesFailed)

	assert.Equal(t, []string{"org/watchtower:1 watchtower-old-a"}, imageEntries(cleanup))
}

// TestRunUpdatesWithNotifications_RemovesOldInstanceImageOnce verifies that a
// session removes the image of an old Watchtower instance found during the
// scan exactly once, and reports it in the image_cleanup event.
func TestRunUpdatesWithNotifications_RemovesOldInstanceImageOnce(t *testing.T) {
	t.Parallel()

	const currentID = "c000000000000000000000000000000000000000000000000000000000000000"

	current := cleanupTestWatchtower(currentID, "watchtower", "org/watchtower:2")
	old := cleanupTestWatchtower("a000000000000000000000000000000000000000000000000000000000000000",
		"watchtower-old-a", "org/watchtower:1")

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{current, old}, nil)
	client.EXPECT().StopAndRemoveContainer(mock.Anything, old, mock.Anything).Return(nil).Once()
	client.EXPECT().IsContainerStale(mock.Anything, mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c types.Container, _ types.UpdateParams) (bool, types.ImageID, string, error) {
			return false, c.ImageID(), "", nil
		}).Maybe()
	client.EXPECT().RemoveImageByID(mock.Anything, types.ImageID("org/watchtower:1"), "org/watchtower:1").
		Return(nil).Once()

	notifier := mockTypes.NewMockNotifier(t)
	notifier.EXPECT().StartNotification(false).Once()
	notifier.EXPECT().ShouldSendNotification(mock.Anything).Return(false).Maybe()

	broadcaster := events.NewBroadcaster()
	published, _ := broadcaster.SubscribeWithDone()

	RunUpdatesWithNotifications(t.Context(), RunUpdatesWithNotificationsParams{
		Logger:           newTestLogger(t),
		Client:           client,
		Notifier:         notifier,
		EventBroadcaster: broadcaster,
		Update:           types.UpdateParams{Cleanup: true, CurrentContainerID: currentID},
	})

	// Events are published synchronously into the subscriber's buffer.
	var cleanup []events.ImageCleanupEntry

	for len(published) > 0 {
		if event := <-published; event.Type == "image_cleanup" {
			data, ok := event.Data.(events.ImageCleanupData)
			require.True(t, ok)

			cleanup = append(cleanup, data.Images...)
		}
	}

	assert.Equal(t, []events.ImageCleanupEntry{{
		ImageID:       "org/watchtower:1",
		ImageName:     "org/watchtower:1",
		ContainerID:   "a000000000000000000000000000000000000000000000000000000000000000",
		ContainerName: "watchtower-old-a",
	}}, cleanup)
}
