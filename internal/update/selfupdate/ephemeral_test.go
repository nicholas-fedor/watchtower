package selfupdate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/observability/logging"
	"github.com/nicholas-fedor/watchtower/internal/testutil/logcapture"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	updateParams "github.com/nicholas-fedor/watchtower/internal/update/params"
	mockSelfupdate "github.com/nicholas-fedor/watchtower/internal/update/selfupdate/mocks"
)

// watchtowerLabels marks a container as a Watchtower container.
var watchtowerLabels = map[string]string{"com.centurylinklabs.watchtower": "true"}

// newTestLogger returns a logger that writes each event to the test's log, so a
// failing test shows what it logged.
//
// Parameters:
//   - t: Test that receives the log lines.
//
// Returns:
//   - *zerolog.Logger: Logger writing to t.
func newTestLogger(t *testing.T) *zerolog.Logger {
	t.Helper()

	// Frame 4 skips zerolog's write path, so each line names the log call.
	w := zerolog.TestWriter{T: t, Frame: 4}

	return new(zerolog.New(w))
}

// createDefaultMockContainer wraps mockUpdate.CreateMockContainerWithConfig with
// common defaults for ephemeral self-update tests. It sets the container to running,
// not restarting, on watchtower:latest, with a current timestamp and the provided labels.
//
// Parameters:
//   - id: Container ID for the mock.
//   - labels: Label map to apply to the container config.
//
// Returns:
//   - *container.Container: Configured mock container.
func createDefaultMockContainer(id string, labels map[string]string) *container.Container {
	return mockUpdate.CreateMockContainerWithConfig(
		id,
		"watchtower",
		"watchtower:latest",
		true,
		false,
		time.Now(),
		&dockerContainer.Config{Image: "watchtower:latest", Labels: labels},
	)
}

// expectOrchestrator expects one orchestrator to be created for source with
// the given container chain and cleanup setting, and answers it with err.
//
// StartOrchestrator takes an interface with only the creation method, so it
// cannot stop, start or rename the source itself.
//
// Parameters:
//   - t: Test the mock belongs to.
//   - source: Watchtower container being replaced.
//   - chain: Container chain the orchestrator is expected to receive.
//   - cleanup: Cleanup setting the orchestrator is expected to receive.
//   - err: Error the creation returns.
//
// Returns:
//   - *mockSelfupdate.MockOrchestratorCreator: Mocked orchestrator creator.
func expectOrchestrator(t *testing.T, source *container.Container, chain string, cleanup bool, err error) *mockSelfupdate.MockOrchestratorCreator {
	t.Helper()

	client := mockSelfupdate.NewMockOrchestratorCreator(t)
	client.EXPECT().
		CreateEphemeralOrchestrator(mock.Anything, source, "watchtower:latest", chain, cleanup).
		Return(container.ContainerID("orchestrator-id"), err).Once()

	return client
}

// TestStartOrchestrator verifies that an ephemeral self-update only creates
// the orchestrator, passing it the new image, the container chain extended
// with the source container, and the cleanup setting, and that it reports the
// source as handed off.
func TestStartOrchestrator(t *testing.T) {
	t.Parallel()

	t.Run("the orchestrator is created successfully", func(t *testing.T) {
		t.Parallel()

		t.Run("should return empty container ID and true so the dying process skips cleanup", func(t *testing.T) {
			t.Parallel()

			source := createDefaultMockContainer("source-123", watchtowerLabels)
			client := expectOrchestrator(t, source, "source-123", false, nil)

			newID, renamed, err := StartOrchestrator(newTestLogger(t), t.Context(), client, source, updateParams.Params{})
			require.NoError(t, err)
			assert.Empty(t, newID)
			assert.True(t, renamed)
		})
	})

	t.Run("the source container has no existing chain", func(t *testing.T) {
		t.Parallel()

		t.Run("should use the source container ID as the new chain", func(t *testing.T) {
			t.Parallel()

			source := createDefaultMockContainer("source-456", watchtowerLabels)
			client := expectOrchestrator(t, source, "source-456", false, nil)

			newID, renamed, err := StartOrchestrator(newTestLogger(t), t.Context(), client, source, updateParams.Params{})
			require.NoError(t, err)
			assert.Empty(t, newID)
			assert.True(t, renamed)
		})
	})

	t.Run("the source container has an existing chain", func(t *testing.T) {
		t.Parallel()

		t.Run("should append the source ID to the existing chain", func(t *testing.T) {
			t.Parallel()

			const existingChain = "old-id-1,old-id-2"

			source := createDefaultMockContainer("source-789", map[string]string{
				"com.centurylinklabs.watchtower":                 "true",
				"com.centurylinklabs.watchtower.container-chain": existingChain,
			})

			chain, present := source.GetContainerChain()
			require.True(t, present)
			require.Equal(t, existingChain, chain)

			client := expectOrchestrator(t, source, "old-id-1,old-id-2,source-789", false, nil)

			newID, renamed, err := StartOrchestrator(newTestLogger(t), t.Context(), client, source, updateParams.Params{})
			require.NoError(t, err)
			assert.Empty(t, newID)
			assert.True(t, renamed)
		})
	})

	t.Run("CreateEphemeralOrchestrator fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should return the orchestrator's error unchanged", func(t *testing.T) {
			t.Parallel()

			createErr := errors.New("failed to start ephemeral orchestrator container: start refused")
			source := createDefaultMockContainer("source-err", watchtowerLabels)
			client := expectOrchestrator(t, source, "source-err", false, createErr)

			_, _, err := StartOrchestrator(newTestLogger(t), t.Context(), client, source, updateParams.Params{})
			require.Same(t, createErr, err)
		})
	})

	t.Run("cleanup is enabled on the update params", func(t *testing.T) {
		t.Parallel()

		t.Run("should pass cleanup=true to the orchestrator", func(t *testing.T) {
			t.Parallel()

			source := createDefaultMockContainer("source-cleanup", watchtowerLabels)
			client := expectOrchestrator(t, source, "source-cleanup", true, nil)

			_, renamed, err := StartOrchestrator(newTestLogger(t), t.Context(), client, source, updateParams.Params{Cleanup: true})
			require.NoError(t, err)
			assert.True(t, renamed)
		})
	})

	// The self-update only starts the orchestrator. Replacement is asynchronous.
	t.Run("the orchestrator launches successfully", func(t *testing.T) {
		t.Parallel()

		t.Run("returns without performing stop or start on the source client path", func(t *testing.T) {
			t.Parallel()

			// StartOrchestrator's interface holds only the orchestrator
			// creation, so it cannot stop, start or rename the source.
			source := createDefaultMockContainer("source-async", watchtowerLabels)
			client := expectOrchestrator(t, source, "source-async", false, nil)

			newID, renamed, err := StartOrchestrator(newTestLogger(t), t.Context(), client, source, updateParams.Params{})
			require.NoError(t, err)
			assert.Empty(t, newID)
			assert.True(t, renamed)
		})
	})
}

// TestEphemeralSelfUpdate_HandoffLog verifies the line that notifications
// include during an ephemeral self-update. Once the orchestrator has started,
// "Starting Watchtower ephemeral self-update" is logged at info with the
// container and the orchestrator's ID. No stop is reported, since the
// orchestrator stops the container later. A failed orchestrator logs nothing
// above debug, since the restart phase reports the container's failed update.
func TestEphemeralSelfUpdate_HandoffLog(t *testing.T) {
	t.Parallel()

	// handoffEntry is a logged line above debug.
	type handoffEntry struct {
		Level          string `json:"level"`
		Message        string `json:"message"`
		Container      string `json:"container"`
		OrchestratorID string `json:"orchestrator_id"`
	}

	source := createDefaultMockContainer("source-ephemeral", watchtowerLabels)

	tests := []struct {
		name string
		// createErr fails the orchestrator creation when set.
		createErr error
		want      []handoffEntry
	}{
		{
			name: "orchestrator started",
			want: []handoffEntry{{
				Level:          "info",
				Message:        logging.EphemeralSelfUpdateStarting,
				Container:      "watchtower",
				OrchestratorID: "orchestrator",
			}},
		},
		{name: "orchestrator failed", createErr: errors.New("create failed"), want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := expectOrchestrator(t, source, "source-ephemeral", false, tt.createErr)

			log, buf := logcapture.NewJSON(zerolog.InfoLevel)

			_, _, err := StartOrchestrator(log, t.Context(), client, source, updateParams.Params{})
			if tt.createErr != nil {
				require.ErrorIs(t, err, tt.createErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.want, logcapture.Entries[handoffEntry](t, buf.String()))
		})
	}
}

// TestEphemeralSelfUpdate_OutlivesCanceledUpdate verifies that the orchestrator
// is created with a context the update's cancellation does not reach, so a
// canceled update still hands Watchtower off, and that the context keeps the
// update context's values.
func TestEphemeralSelfUpdate_OutlivesCanceledUpdate(t *testing.T) {
	t.Parallel()

	// parentKey is a context key only this test uses.
	type parentKey struct{}

	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), parentKey{}, "parent-value"))
	cancel()

	source := createDefaultMockContainer("source-canceled", map[string]string{
		"com.centurylinklabs.watchtower": "true",
	})

	client := mockSelfupdate.NewMockOrchestratorCreator(t)
	client.EXPECT().
		CreateEphemeralOrchestrator(
			mock.MatchedBy(func(created context.Context) bool {
				_, hasDeadline := created.Deadline()

				return created.Err() == nil && hasDeadline && created.Value(parentKey{}) == "parent-value"
			}),
			source, "watchtower:latest", "source-canceled", false,
		).
		Return(container.ContainerID("orchestrator-id"), nil).Once()

	_, renamed, err := StartOrchestrator(newTestLogger(t), ctx, client, source, updateParams.Params{})
	require.NoError(t, err)
	assert.True(t, renamed)
}
