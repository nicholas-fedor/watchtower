package selfupdate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	mockSelfupdate "github.com/nicholas-fedor/watchtower/internal/update/selfupdate/mocks"
)

// recoveryContainer returns a Watchtower container for the orphan recovery
// tests.
//
// Parameters:
//   - id: Container ID.
//   - name: Container name.
//   - running: Whether the container is running. A container that is not
//     running is in the created state.
//   - scope: Scope label value. Empty leaves the container without a scope.
//
// Returns:
//   - *container.Container: Configured mock container.
func recoveryContainer(id, name string, running bool, scope string) *container.Container {
	labels := map[string]string{"com.centurylinklabs.watchtower": "true"}
	if scope != "" {
		labels["com.centurylinklabs.watchtower.scope"] = scope
	}

	ctr := mockUpdate.CreateMockContainerWithConfig(
		id,
		name,
		"watchtower:latest",
		running,
		false,
		time.Now(),
		&dockerContainer.Config{Image: "watchtower:latest", Labels: labels},
	)

	if !running {
		ctr.ContainerInfo().State.Status = dockerContainer.StateCreated
	}

	return ctr
}

// expectListed expects one listing of all containers and answers it with
// containers.
//
// Parameters:
//   - client: Mocked container client.
//   - containers: Containers the listing returns.
func expectListed(client *mockSelfupdate.MockRecoveryClient, containers ...*container.Container) {
	client.EXPECT().ListLabeledContainers(mock.Anything, labels.Watchtower).Return(containers, nil).Once()
}

// TestRetireOldInstance verifies that retiring an old Watchtower container
// starts an orphaned replacement, if there is one, and disables the old
// container's restarts. Both steps get a live context with a deadline even
// when the caller's context is already canceled.
func TestRetireOldInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		orphan bool
	}{
		{name: "with an orphaned replacement", orphan: true},
		{name: "without an orphaned replacement", orphan: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			old := recoveryContainer("old-id", "/watchtower-old-000000000000", true, "")
			listed := []*container.Container{old}

			// liveContext matches a context with a deadline that the caller's
			// cancellation did not reach.
			liveContext := mock.MatchedBy(func(ctx context.Context) bool {
				_, bounded := ctx.Deadline()

				return bounded && ctx.Err() == nil
			})

			client := mockSelfupdate.NewMockRetireClient(t)

			if tt.orphan {
				orphan := recoveryContainer("orphan-id", "/watchtower", false, "")
				listed = append(listed, orphan)

				client.EXPECT().StartContainerByID(liveContext, orphan.ID()).Return(nil).Once()
			}

			client.EXPECT().ListLabeledContainers(liveContext, labels.Watchtower).Return(listed, nil).Once()
			client.EXPECT().SetRestartPolicy(liveContext, old.ID(),
				dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled}).Return(nil).Once()

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			// The other recovery lines are at debug, so an info capture holds
			// only the line reporting the recovery.
			log, buf := logcapture.NewJSON(zerolog.InfoLevel)
			RetireOldInstance(log, ctx, client, old, time.Minute)

			type recoveryEntry struct {
				Message   string `json:"message"`
				Container string `json:"container"`
			}

			var want []recoveryEntry
			if tt.orphan {
				want = []recoveryEntry{{Message: logging.OrphanRecovered, Container: "watchtower"}}
			}

			assert.Equal(t, want, logcapture.Entries[recoveryEntry](t, buf.String()))
		})
	}
}

// Test_tryRecoverOrphanedContainer verifies that a Watchtower replacement left
// in the created state is started. Recovery gives up quietly when listing
// fails or when no orphan in the current container's scope exists, and reports
// a start that fails. The client mocks expect exactly the starts each case makes.
func Test_tryRecoverOrphanedContainer(t *testing.T) {
	t.Parallel()

	t.Run("listing containers fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should start nothing", func(t *testing.T) {
			t.Parallel()

			client := mockSelfupdate.NewMockRecoveryClient(t)
			client.EXPECT().ListLabeledContainers(mock.Anything, labels.Watchtower).Return(nil, context.DeadlineExceeded).Once()

			current := mockUpdate.CreateMockContainer("current-id", "watchtower", "watchtower:latest", time.Now())

			tryRecoverOrphanedContainer(newTestLogger(t), t.Context(), client, current)
		})
	})

	t.Run("no orphaned container exists", func(t *testing.T) {
		t.Parallel()

		t.Run("should start nothing", func(t *testing.T) {
			t.Parallel()

			// The mock has no start expectation, so starting a container fails
			// the test.
			oldContainer := recoveryContainer("old-id", "watchtower-old-abc123", true, "")
			current := mockUpdate.CreateMockContainer("current-id", "watchtower", "watchtower:latest", time.Now())

			client := mockSelfupdate.NewMockRecoveryClient(t)
			expectListed(client, oldContainer, current)

			tryRecoverOrphanedContainer(newTestLogger(t), t.Context(), client, current)
		})
	})

	t.Run("an orphaned created-state Watchtower container exists", func(t *testing.T) {
		t.Parallel()

		t.Run("should start it", func(t *testing.T) {
			t.Parallel()

			orphaned := recoveryContainer("orphaned-id", "watchtower-xyz", false, "")
			current := recoveryContainer("current-id", "watchtower-old-abc123", true, "")

			client := mockSelfupdate.NewMockRecoveryClient(t)
			expectListed(client, orphaned, current)
			client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("orphaned-id")).Return(nil).Once()

			tryRecoverOrphanedContainer(newTestLogger(t), t.Context(), client, current)
		})
	})

	t.Run("start fails for the orphaned container", func(t *testing.T) {
		t.Parallel()

		t.Run("should report the failure when no other orphan exists", func(t *testing.T) {
			t.Parallel()

			orphaned := recoveryContainer("orphaned-id", "watchtower-xyz", false, "")
			current := recoveryContainer("current-id", "watchtower-old-abc123", true, "")

			client := mockSelfupdate.NewMockRecoveryClient(t)
			expectListed(client, orphaned, current)
			client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("orphaned-id")).
				Return(context.DeadlineExceeded).Once()

			log, buf := logcapture.NewJSON(zerolog.InfoLevel)
			tryRecoverOrphanedContainer(log, t.Context(), client, current)

			// No Watchtower may run once the old instance exits, so the failure
			// is reported at error with its cause.
			type failureEntry struct {
				Level   string `json:"level"`
				Message string `json:"message"`
				Error   string `json:"error"`
			}

			want := []failureEntry{{
				Level:   "error",
				Message: logging.OrphanStartFailed,
				Error:   context.DeadlineExceeded.Error(),
			}}
			assert.Equal(t, want, logcapture.Entries[failureEntry](t, buf.String()))
		})

		t.Run("should start the next orphan", func(t *testing.T) {
			t.Parallel()

			first := recoveryContainer("first-orphan-id", "watchtower-xyz", false, "")
			second := recoveryContainer("second-orphan-id", "watchtower-abc", false, "")
			current := recoveryContainer("current-id", "watchtower-old-abc123", true, "")

			client := mockSelfupdate.NewMockRecoveryClient(t)
			expectListed(client, first, second, current)
			client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("first-orphan-id")).
				Return(context.DeadlineExceeded).Once()
			client.EXPECT().StartContainerByID(mock.Anything, container.ContainerID("second-orphan-id")).
				Return(nil).Once()

			// The attempts log at debug, so an info capture holds the failed
			// start of the first orphan and the recovery of the second.
			log, buf := logcapture.NewJSON(zerolog.InfoLevel)
			tryRecoverOrphanedContainer(log, t.Context(), client, current)

			type recoveryEntry struct {
				Level     string `json:"level"`
				Message   string `json:"message"`
				Container string `json:"container"`
				ID        string `json:"id"`
			}

			want := []recoveryEntry{
				{
					Level:     "error",
					Message:   logging.OrphanStartFailed,
					Container: "watchtower-xyz",
					ID:        container.ContainerID("first-orphan-id").ShortID(),
				},
				{
					Level:     "info",
					Message:   logging.OrphanRecovered,
					Container: "watchtower-abc",
					ID:        container.ContainerID("second-orphan-id").ShortID(),
				},
			}
			assert.Equal(t, want, logcapture.Entries[recoveryEntry](t, buf.String()))
		})
	})

	t.Run("the orphaned container is in a different scope", func(t *testing.T) {
		t.Parallel()

		t.Run("should not start it", func(t *testing.T) {
			t.Parallel()

			orphaned := recoveryContainer("orphaned-id", "watchtower-xyz", false, "scope-b")
			current := recoveryContainer("current-id", "watchtower-old-abc123", true, "scope-a")

			client := mockSelfupdate.NewMockRecoveryClient(t)
			expectListed(client, orphaned, current)

			tryRecoverOrphanedContainer(newTestLogger(t), t.Context(), client, current)
		})
	})

	t.Run("the current container has no scope and the orphaned container has an explicit scope", func(t *testing.T) {
		t.Parallel()

		t.Run("should not start it", func(t *testing.T) {
			t.Parallel()

			orphaned := recoveryContainer("orphaned-id", "watchtower-xyz", false, "scope-b")
			current := recoveryContainer("current-id", "watchtower-old-abc123", true, "")

			client := mockSelfupdate.NewMockRecoveryClient(t)
			expectListed(client, orphaned, current)

			tryRecoverOrphanedContainer(newTestLogger(t), t.Context(), client, current)
		})
	})
}

// TestDisableOldRestarts verifies that the restart policy of an old Watchtower
// container is set to "no". A failure is logged only at debug, since an old
// container that starts again exits on its own.
func TestDisableOldRestarts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		updateErr error
		wantLog   bool
	}{
		{name: "the update succeeds", wantLog: false},
		{name: "the update fails", updateErr: errors.New("update refused"), wantLog: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			oldContainer := mockUpdate.CreateMockContainer("old-id", "/watchtower-old-000000000000", "nickfedor/watchtower:latest", time.Now())

			client := mockSelfupdate.NewMockRestartPolicySetter(t)
			client.EXPECT().SetRestartPolicy(mock.Anything, oldContainer.ID(),
				dockerContainer.RestartPolicy{Name: dockerContainer.RestartPolicyDisabled}).Return(tt.updateErr).Once()

			log, logOutput := logcapture.New(zerolog.DebugLevel)
			DisableOldRestarts(log, t.Context(), oldContainer, client)

			if tt.wantLog {
				assert.Contains(t, logOutput.String(), "Failed to disable the restart policy of the old Watchtower container")
				assert.Contains(t, logOutput.String(), "update refused")
				assert.Contains(t, logOutput.String(), "level=debug")
			} else {
				assert.Empty(t, logOutput.String())
			}
		})
	}
}
