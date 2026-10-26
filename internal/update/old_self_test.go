package update

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	dockerContainer "github.com/moby/moby/api/types/container"

	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/observability/events"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// TestRunUpdatesWithNotifications_OldSelfDetected verifies that when the
// update finds that Watchtower runs in an old instance's container, it reports
// that through OnOldSelfDetected and sends no notification, since nothing
// failed. The old instance's restart policy is disabled first, and the scan
// ends with an empty scan_completed event so subscribers see it finish.
func TestRunUpdatesWithNotifications_OldSelfDetected(t *testing.T) {
	const oldID = "01d0000000000000000000000000000000000000000000000000000000000000"

	oldSelf := mockUpdate.CreateMockContainerWithConfig(
		oldID,
		"/watchtower-old-01d000000000",
		"nickfedor/watchtower:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{
			Image:  "nickfedor/watchtower:latest",
			Labels: map[string]string{"com.centurylinklabs.watchtower": "true"},
		},
	)

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything, mock.Anything).Return([]types.Container{oldSelf}, nil)
	client.EXPECT().SetRestartPolicy(mock.Anything, oldSelf.ID(),
		dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled}).Return(nil).Once()

	// The notifier mock fails the test on any call other than starting the batch.
	notifier := mockTypes.NewMockNotifier(t)
	notifier.EXPECT().StartNotification(false).Once()

	broadcaster := events.NewBroadcaster()
	published, _ := broadcaster.SubscribeWithDone()

	var detected int

	RunUpdatesWithNotifications(t.Context(), RunUpdatesWithNotificationsParams{
		Logger:            newTestLogger(t),
		Client:            client,
		Notifier:          notifier,
		EventBroadcaster:  broadcaster,
		Update:            types.UpdateParams{CurrentContainerID: types.ContainerID(oldID)},
		OnOldSelfDetected: func() { detected++ },
	})

	assert.Equal(t, 1, detected, "OnOldSelfDetected is called once")

	// Events are published synchronously into the subscriber's buffer.
	var received []events.Event

	for len(published) > 0 {
		received = append(received, <-published)
	}

	if assert.Len(t, received, 2) {
		assert.Equal(t, "scan_started", received[0].Type)
		assert.Equal(t, "scan_completed", received[1].Type)
		assert.Equal(t, events.ScanCompletedData{}, received[1].Data)
	}
}
