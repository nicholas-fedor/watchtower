package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cerrdefs "github.com/containerd/errdefs"
	dockerContainer "github.com/moby/moby/api/types/container"
	dockerImage "github.com/moby/moby/api/types/image"

	mockContainerClient "github.com/nicholas-fedor/watchtower/internal/container/client/mocks"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/testutil/containerfixture"
	"github.com/nicholas-fedor/watchtower/internal/testutil/modelfixture"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestRemoveOrphanedOrchestrators verifies that orphaned orchestrators are
// listed by their label and removed, and how a failed list or removal is
// reported.
func TestRemoveOrphanedOrchestrators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// orphaned marks an orchestrator as running long past any self-update.
	orphaned := containerfixture.WithContainerState(dockerContainer.State{
		Running:   true,
		Status:    "running",
		StartedAt: "2000-01-01T00:00:00Z",
	})

	t.Run("there are no containers", func(t *testing.T) {
		t.Parallel()

		t.Run("should return 0 removed and no error", func(t *testing.T) {
			t.Parallel()

			mockAPIClient := mockContainerClient.NewMockClient(t)
			mockAPIClient.EXPECT().
				ListContainers(ctx).
				Return([]types.Container{}, nil)

			count, err := RemoveOrphanedOrchestrators(newTestLogger(t), ctx, mockAPIClient)

			require.NoError(t, err)
			assert.Equal(t, 0, count)
		})
	})

	t.Run("there are containers but none are orchestrators", func(t *testing.T) {
		t.Parallel()

		t.Run("should return 0 removed and no error", func(t *testing.T) {
			t.Parallel()

			regularContainer := modelfixture.Container(
				containerfixture.WithID("regular123"),
				containerfixture.WithName("regular-container"),
				containerfixture.WithLabels(map[string]string{
					"com.centurylinklabs.watchtower": "true",
				}),
			)

			mockAPIClient := mockContainerClient.NewMockClient(t)
			mockAPIClient.EXPECT().
				ListContainers(ctx).
				Return([]types.Container{regularContainer}, nil)

			count, err := RemoveOrphanedOrchestrators(newTestLogger(t), ctx, mockAPIClient)

			require.NoError(t, err)
			assert.Equal(t, 0, count)
		})
	})

	t.Run("there is an orphaned orchestrator", func(t *testing.T) {
		t.Parallel()

		t.Run("should remove the orchestrator and return 1", func(t *testing.T) {
			t.Parallel()

			orchestrator := modelfixture.Container(
				containerfixture.WithID("orch123"),
				containerfixture.WithName("watchtower-orchestrator-abc"),
				containerfixture.WithLabels(map[string]string{
					labels.EphemeralOrchestrator: "true",
				}),
				orphaned,
			)

			mockAPIClient := mockContainerClient.NewMockClient(t)
			mockAPIClient.EXPECT().
				ListContainers(ctx).
				Return([]types.Container{orchestrator}, nil)
			mockAPIClient.EXPECT().
				StopAndRemoveContainer(
					ctx,
					orchestrator,
					mock.AnythingOfType("time.Duration"),
				).
				Return(nil)

			count, err := RemoveOrphanedOrchestrators(newTestLogger(t), ctx, mockAPIClient)

			require.NoError(t, err)
			assert.Equal(t, 1, count)
		})
	})

	t.Run("there are multiple orphaned orchestrators", func(t *testing.T) {
		t.Parallel()

		t.Run("should remove all orchestrators and return the count", func(t *testing.T) {
			t.Parallel()

			orch1 := modelfixture.Container(
				containerfixture.WithID("orch001"),
				containerfixture.WithName("watchtower-orchestrator-001"),
				containerfixture.WithLabels(map[string]string{
					labels.EphemeralOrchestrator: "true",
				}),
				orphaned,
			)
			orch2 := modelfixture.Container(
				containerfixture.WithID("orch002"),
				containerfixture.WithName("watchtower-orchestrator-002"),
				containerfixture.WithLabels(map[string]string{
					labels.EphemeralOrchestrator: "true",
				}),
				orphaned,
			)
			regular := modelfixture.Container(
				containerfixture.WithID("regular1"),
				containerfixture.WithName("app-container"),
			)

			mockAPIClient := mockContainerClient.NewMockClient(t)
			mockAPIClient.EXPECT().
				ListContainers(ctx).
				Return([]types.Container{orch1, regular, orch2}, nil)
			mockAPIClient.EXPECT().
				StopAndRemoveContainer(
					ctx,
					orch1,
					mock.AnythingOfType("time.Duration"),
				).
				Return(nil)
			mockAPIClient.EXPECT().
				StopAndRemoveContainer(
					ctx,
					orch2,
					mock.AnythingOfType("time.Duration"),
				).
				Return(nil)

			count, err := RemoveOrphanedOrchestrators(newTestLogger(t), ctx, mockAPIClient)

			require.NoError(t, err)
			assert.Equal(t, 2, count)
		})
	})

	t.Run("ListContainers fails", func(t *testing.T) {
		t.Parallel()

		t.Run("should return an error", func(t *testing.T) {
			t.Parallel()

			mockAPIClient := mockContainerClient.NewMockClient(t)
			mockAPIClient.EXPECT().
				ListContainers(ctx).
				Return(nil, errors.New("docker daemon unavailable"))

			count, err := RemoveOrphanedOrchestrators(newTestLogger(t), ctx, mockAPIClient)

			require.ErrorContains(t, err, "failed to list containers")
			assert.Equal(t, 0, count)
		})
	})

	t.Run("StopAndRemoveContainer fails for one orchestrator", func(t *testing.T) {
		t.Parallel()

		t.Run("should continue and remove others, returning partial count", func(t *testing.T) {
			t.Parallel()

			orch1 := modelfixture.Container(
				containerfixture.WithID("orch001"),
				containerfixture.WithName("watchtower-orchestrator-001"),
				containerfixture.WithLabels(map[string]string{
					labels.EphemeralOrchestrator: "true",
				}),
				orphaned,
			)
			orch2 := modelfixture.Container(
				containerfixture.WithID("orch002"),
				containerfixture.WithName("watchtower-orchestrator-002"),
				containerfixture.WithLabels(map[string]string{
					labels.EphemeralOrchestrator: "true",
				}),
				orphaned,
			)

			mockAPIClient := mockContainerClient.NewMockClient(t)
			mockAPIClient.EXPECT().
				ListContainers(ctx).
				Return([]types.Container{orch1, orch2}, nil)
			mockAPIClient.EXPECT().
				StopAndRemoveContainer(
					ctx,
					orch1,
					mock.AnythingOfType("time.Duration"),
				).
				Return(errors.New("removal failed"))
			mockAPIClient.EXPECT().
				StopAndRemoveContainer(
					ctx,
					orch2,
					mock.AnythingOfType("time.Duration"),
				).
				Return(nil)

			count, err := RemoveOrphanedOrchestrators(newTestLogger(t), ctx, mockAPIClient)

			require.NoError(t, err)
			assert.Equal(t, 1, count)
		})
	})

	t.Run("a container has no orchestrator label", func(t *testing.T) {
		t.Parallel()

		t.Run("should skip the container without error", func(t *testing.T) {
			t.Parallel()

			noLabelContainer := modelfixture.Container(
				containerfixture.WithID("no-label"),
				containerfixture.WithName("no-label-container"),
			)

			mockAPIClient := mockContainerClient.NewMockClient(t)
			mockAPIClient.EXPECT().
				ListContainers(ctx).
				Return([]types.Container{noLabelContainer}, nil)

			count, err := RemoveOrphanedOrchestrators(newTestLogger(t), ctx, mockAPIClient)

			require.NoError(t, err)
			assert.Equal(t, 0, count)
		})
	})
}

// TestRemoveOrphanedOrchestrators_SkipsInFlight verifies that startup cleanup
// removes only orchestrators that have run longer than any self-update can
// take. A younger orchestrator may still be handing off to a new Watchtower,
// whether this instance's or another scope's, so it is kept, as is one whose
// start time cannot be read.
func TestRemoveOrphanedOrchestrators_SkipsInFlight(t *testing.T) {
	t.Parallel()

	// startedAt formats a start time the way Docker reports it.
	startedAt := func(age time.Duration) string {
		return time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
	}

	tests := []struct {
		name string
		// state is the orchestrator's Docker state.
		state dockerContainer.State
		// created is the orchestrator's creation time, used without a start time.
		created string
		// wantRemoved reports whether the orchestrator is removed.
		wantRemoved bool
	}{
		{
			name:  "just started",
			state: dockerContainer.State{Running: true, StartedAt: startedAt(time.Second)},
		},
		{
			name:  "within the self-update window",
			state: dockerContainer.State{Running: true, StartedAt: startedAt(4 * time.Minute)},
		},
		{
			name:        "past the self-update window",
			state:       dockerContainer.State{Running: true, StartedAt: startedAt(11 * time.Minute)},
			wantRemoved: true,
		},
		{
			name:  "start time in the future",
			state: dockerContainer.State{Running: true, StartedAt: startedAt(-time.Minute)},
		},
		{
			name:  "unreadable start time",
			state: dockerContainer.State{Running: true, StartedAt: "not-a-time"},
		},
		{
			name:        "no start time and old creation time",
			state:       dockerContainer.State{Running: true, StartedAt: "0001-01-01T00:00:00Z"},
			created:     startedAt(time.Hour),
			wantRemoved: true,
		},
		{
			name:    "no start time and recent creation time",
			state:   dockerContainer.State{Running: true},
			created: startedAt(time.Minute),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			orchestrator := modelfixture.Container(
				containerfixture.WithID("orch123"),
				containerfixture.WithName("watchtower-orchestrator"),
				containerfixture.WithLabels(map[string]string{labels.EphemeralOrchestrator: "true"}),
				containerfixture.WithContainerState(tt.state),
				func(c *dockerContainer.InspectResponse, _ *dockerImage.InspectResponse) {
					c.Created = tt.created
				},
			)
			// A container without the orchestrator label is never removed.
			app := modelfixture.Container(containerfixture.WithID("app123"), containerfixture.WithName("app"))

			client := mockContainerClient.NewMockClient(t)
			client.EXPECT().ListContainers(mock.Anything).
				Return([]types.Container{orchestrator, app}, nil).Once()

			wantCount := 0

			if tt.wantRemoved {
				wantCount = 1

				client.EXPECT().StopAndRemoveContainer(mock.Anything, orchestrator, time.Duration(0)).
					Return(nil).Once()
			}

			count, err := RemoveOrphanedOrchestrators(newTestLogger(t), t.Context(), client)
			require.NoError(t, err)
			assert.Equal(t, wantCount, count)
		})
	}
}

// TestRemoveOrphanedOrchestrators_AlreadyGone verifies that an orphaned
// orchestrator which no longer exists when cleanup reaches it, for example one
// Docker removed on exit, is not reported as a failed removal.
func TestRemoveOrphanedOrchestrators_AlreadyGone(t *testing.T) {
	t.Parallel()

	orchestrator := modelfixture.Container(
		containerfixture.WithID("orch123"),
		containerfixture.WithName("watchtower-orchestrator"),
		containerfixture.WithLabels(map[string]string{labels.EphemeralOrchestrator: "true"}),
		containerfixture.WithContainerState(dockerContainer.State{
			Running:   true,
			StartedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		}),
	)

	client := mockContainerClient.NewMockClient(t)
	client.EXPECT().ListContainers(mock.Anything).Return([]types.Container{orchestrator}, nil).Once()
	client.EXPECT().StopAndRemoveContainer(mock.Anything, orchestrator, time.Duration(0)).
		Return(fmt.Errorf("no such container: %w", cerrdefs.ErrNotFound)).Once()

	var out bytes.Buffer

	log := zerolog.New(&out).Level(zerolog.WarnLevel)

	count, err := RemoveOrphanedOrchestrators(&log, t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.Empty(t, out.String(), "a missing orchestrator is not a warning")
}
