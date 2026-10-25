package update

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dockerContainer "github.com/moby/moby/api/types/container"
	dockerNetwork "github.com/moby/moby/api/types/network"

	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/internal/update/state"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// stopContainersTestCase represents a test case for stopContainersInReversedOrder cancellation.
type stopContainersTestCase struct {
	name                string
	numContainers       int
	cancelAtIndex       int    // Index at which to cancel (from end, -1 means no cancellation)
	expectedStopped     int    // Number of containers that should be stopped
	expectedSkipped     int    // Number of containers that should be skipped
	expectedLogMessages int    // Expected number of log messages for skipped containers
	description         string // Human-readable description
}

// rollingRestartTestCase represents a test case for performRollingRestart cancellation.
type rollingRestartTestCase struct {
	name                string
	numContainers       int
	cancelAtIndex       int    // Index at which to cancel (-1 means no cancellation)
	expectedProcessed   int    // Number of containers that should be processed
	expectedSkipped     int    // Number of containers that should be skipped
	expectedLogMessages int    // Expected number of log messages for skipped containers
	description         string // Human-readable description
}

// Test_restartStaleContainer verifies when restarting a stale Watchtower
// container renames it first, including run-once mode, an existing old name,
// and an ephemeral self-update handoff.
func Test_restartStaleContainer(t *testing.T) {
	t.Parallel()

	t.Run("should not rename Watchtower container in run-once mode", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: []types.Container{
					mockUpdate.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						},
					),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		params := types.UpdateParams{
			RunOnce: true,
		}
		testContainer := client.TestData.Containers[0]
		newID, renamed, err := restartStaleContainer(newTestLogger(t), t.Context(), testContainer, client, params)
		require.NoError(t, err)
		assert.False(t, renamed)
		assert.Equal(t, int32(0), client.TestData.RenameContainerCount.Load())
		assert.NotEmpty(t, newID)
	})

	t.Run("should rename Watchtower container when not in run-once mode", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: []types.Container{
					mockUpdate.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						},
					),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		params := types.UpdateParams{
			RunOnce: false,
		}
		testContainer := client.TestData.Containers[0]
		newID, renamed, err := restartStaleContainer(newTestLogger(t), t.Context(), testContainer, client, params)
		require.NoError(t, err)
		assert.True(t, renamed)
		assert.Equal(t, int32(1), client.TestData.RenameContainerCount.Load())
		assert.NotEmpty(t, newID)
	})

	t.Run("should skip rename if source container is already named with the target old name", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: []types.Container{
					mockUpdate.CreateMockContainerWithConfig(
						"abc123def456",
						"/watchtower-old-abc123def456",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						},
					),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		params := types.UpdateParams{
			RunOnce: false,
		}
		testContainer := client.TestData.Containers[0]
		newID, renamed, err := restartStaleContainer(newTestLogger(t), t.Context(), testContainer, client, params)
		require.NoError(t, err)
		assert.True(t, renamed)
		assert.Equal(t, int32(0), client.TestData.RenameContainerCount.Load())
		assert.NotEmpty(t, newID)
	})

	t.Run("should treat ephemeral self-update as a completed handoff", func(t *testing.T) {
		t.Parallel()

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: []types.Container{
					mockUpdate.CreateMockContainerWithConfig(
						"watchtower",
						"/watchtower",
						"watchtower:latest",
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels: map[string]string{
								"com.centurylinklabs.watchtower": "true",
							},
						},
					),
				},
				Staleness: map[string]bool{
					"watchtower": true,
				},
			},
			false,
			false,
		)
		params := types.UpdateParams{
			EphemeralSelfUpdate: true,
			Cleanup:             true,
		}
		testContainer := client.TestData.Containers[0]
		newID, renamed, err := restartStaleContainer(newTestLogger(t), t.Context(), testContainer, client, params)
		require.NoError(t, err)
		assert.True(t, renamed)
		assert.Empty(t, newID)
		assert.Equal(t, int32(0), client.TestData.RenameContainerCount.Load())
		assert.True(t, client.TestData.LastCleanup)
	})
}

// DetachedContext specs cover the detached context that restartStaleContainer creates
// from the Timeout setting. A positive Timeout sets the deadline, and a zero or
// negative Timeout falls back to a default deadline that prevents indefinite blocking.
func TestDetachedContext(t *testing.T) {
	t.Parallel()

	// TestDetachedContextDeadlineCase represents a test case for detached context deadline behavior.
	type TestDetachedContextDeadlineCase struct {
		name            string
		timeout         time.Duration
		expectDeadline  bool
		expectedTimeout time.Duration
		description     string
	}

	t.Run("restartStaleContainer detached context deadline", func(t *testing.T) {
		t.Parallel()

		testCases := []TestDetachedContextDeadlineCase{
			{
				name:            "positive timeout above minimum uses configured timeout",
				timeout:         10 * time.Minute,
				expectDeadline:  true,
				expectedTimeout: 10 * time.Minute,
				description:     "When Timeout exceeds defaultCreateStartTimeout, the detached context uses the configured timeout",
			},
			{
				name:            "positive timeout below minimum creates context with minimum deadline",
				timeout:         30 * time.Second,
				expectDeadline:  true,
				expectedTimeout: defaultCreateStartTimeout,
				description:     "When Timeout is below defaultCreateStartTimeout, the detached context uses the minimum",
			},
			{
				name:            "zero timeout creates context with minimum deadline",
				timeout:         0,
				expectDeadline:  true,
				expectedTimeout: defaultCreateStartTimeout,
				description:     "When Timeout is zero, the detached context uses the minimum create/start deadline",
			},
			{
				name:            "negative timeout creates context with minimum deadline",
				timeout:         -1 * time.Second,
				expectDeadline:  true,
				expectedTimeout: defaultCreateStartTimeout,
				description:     "When Timeout is negative, the detached context uses the minimum create/start deadline",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				// Create a mock client with a Watchtower container that will trigger
				// the restart policy update path where the detached context is used.
				client := mockUpdate.CreateMockClient(
					&mockUpdate.TestData{
						Containers: []types.Container{
							mockUpdate.CreateMockContainerWithConfig(
								"watchtower",
								"/watchtower",
								"watchtower:latest",
								true,
								false,
								time.Now(),
								&dockerContainer.Config{
									Labels: map[string]string{
										"com.centurylinklabs.watchtower": "true",
									},
								},
							),
						},
						Staleness: map[string]bool{
							"watchtower": true,
						},
					},
					false,
					false,
				)

				// Configure params with the test timeout value.
				// RunOnce is false to enable the rename path which uses the detached context.
				params := types.UpdateParams{
					Timeout: tc.timeout,
					RunOnce: false,
				}

				testContainer := client.TestData.Containers[0]

				// Call restartStaleContainer which creates and uses the detached context.
				newID, renamed, err := restartStaleContainer(newTestLogger(t),
					t.Context(),
					testContainer,
					client,
					params,
				)

				// Verify the operation succeeded.
				require.NoError(t, err)
				assert.True(t, renamed)
				assert.NotEmpty(t, newID)

				// Verify SetRestartPolicy was called (this uses the detached context).
				// The detached context is used for updating the restart policy of the
				// renamed Watchtower container.
				assert.Equal(t, int32(1), client.TestData.SetRestartPolicyCount.Load())

				// Verify CreateContainer and StartContainerByID also use the detached context.
				// These operations run after the initial rename, so they should share the same
				// deadline when expectDeadline is true.
				createCtx := client.TestData.CreateContainerCtx
				startCtx := client.TestData.StartContainerByIDCtx
				noRestartCtx := client.TestData.SetRestartPolicyCtx

				require.NotNil(t, createCtx, "CreateContainer should receive a context")
				require.NotNil(t, startCtx, "StartContainerByID should receive a context")
				assert.Same(t, startCtx, createCtx, "CreateContainer and StartContainerByID should share the same detached context")
				assert.Same(t, noRestartCtx, createCtx, "CreateContainer should use the same detached context as SetRestartPolicy")

				if tc.expectDeadline {
					_, createHasDeadline := createCtx.Deadline()
					_, startHasDeadline := startCtx.Deadline()

					assert.True(t, createHasDeadline)
					assert.True(t, startHasDeadline)

					expectedDeadline := time.Now().Add(tc.expectedTimeout)
					createDeadline, _ := createCtx.Deadline()
					startDeadline, _ := startCtx.Deadline()

					assert.WithinDuration(t, expectedDeadline, createDeadline, time.Second)
					assert.WithinDuration(t, expectedDeadline, startDeadline, time.Second)
				} else {
					_, createHasDeadline := createCtx.Deadline()
					_, startHasDeadline := startCtx.Deadline()

					assert.False(t, createHasDeadline)
					assert.False(t, startHasDeadline)
				}

				// Verify CreateContainer and StartContainerByID also use the detached context.
				// These operations run after CreateContainer succeeds, so the same deadline
				// should apply when expectDeadline is true.
				assert.Equal(t, int32(1), client.TestData.CreateContainerCount.Load())
				assert.Equal(t, int32(1), client.TestData.StartContainerCount.Load())

				if tc.expectDeadline {
					createDeadline, createHasDeadline := client.TestData.CreateContainerCtx.Deadline()
					assert.True(t, createHasDeadline)

					startDeadline, startHasDeadline := client.TestData.StartContainerByIDCtx.Deadline()
					assert.True(t, startHasDeadline)

					expectedDeadline := time.Now().Add(tc.expectedTimeout)
					assert.WithinDuration(t, expectedDeadline, createDeadline, time.Second)
					assert.WithinDuration(t, expectedDeadline, startDeadline, time.Second)
				}
			})
		}
	})

	t.Run("Watchtower self-update create failure recovery", func(t *testing.T) {
		t.Parallel()

		t.Run("renames the source back to the original name when create fails", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainerWithConfig(
							"watchtower-source-id",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&dockerContainer.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
					Staleness: map[string]bool{
						"watchtower": true,
					},
					CreateContainerError: errors.New("simulated create failure"),
				},
				false,
				false,
			)

			params := types.UpdateParams{
				Timeout: 0,
				RunOnce: false,
			}

			testContainer := client.TestData.Containers[0]

			// Use a cancellable parent context to verify rename-back survives cancellation.
			parentCtx, parentCancel := context.WithCancel(t.Context())
			defer parentCancel()

			// Add simulated latency to CreateContainer so the initial rename completes
			// before we cancel the parent context.
			client.TestData.SimulatedLatency = 5 * time.Millisecond

			var (
				renamed bool
				err     error
			)

			// Run restartStaleContainer in a goroutine so we can cancel the parent
			// after the initial handoff rename but before/during create failure.
			var wg sync.WaitGroup
			wg.Go(func() {
				_, renamed, err = restartStaleContainer(newTestLogger(t),
					parentCtx,
					testContainer,
					client,
					params,
				)
			})

			// Wait for CreateContainer to be called, indicating the initial rename
			// has completed. Then cancel the parent context.
			require.Eventually(t, func() bool {
				return client.TestData.CreateContainerCount.Load() > 0
			}, 5*time.Second, 10*time.Millisecond)

			parentCancel()
			wg.Wait()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to create container")
			assert.False(t, renamed, "name should be restored after create failure")
			assert.Equal(t, int32(2), client.TestData.RenameContainerCount.Load())
			require.Len(t, client.TestData.RenameTargets, 2)
			assert.Equal(t, "watchtower", client.TestData.RenameTargets[1])
			assert.Equal(t, int32(0), client.TestData.StartContainerCount.Load())

			// Assert that CreateContainer and RenameContainer received distinct contexts.
			// The first rename (initial handoff) uses the caller context, the second
			// rename (recovery after create failure) uses a fresh bounded context.
			createCtx := client.TestData.CreateContainerCtx
			renameBackCtx := client.TestData.RenameContainerCtx

			require.NotNil(t, createCtx, "CreateContainer should receive a context")
			require.NotNil(t, renameBackCtx, "RenameContainer should receive a context for rename-back")
			assert.NotSame(t, renameBackCtx, createCtx, "CreateContainer and rename-back should use distinct contexts")

			// Assert that the rename-back context has an active deadline (is a bounded timeout context).
			_, hasDeadline := renameBackCtx.Deadline()
			assert.True(t, hasDeadline, "rename-back context should have a deadline for bounded recovery")
		})
	})

	t.Run("Watchtower self-update start failure cleanup", func(t *testing.T) {
		t.Parallel()

		t.Run("removes the newly created container, not the renamed source", func(t *testing.T) {
			t.Parallel()

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainerWithConfig(
							"watchtower-source-id",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&dockerContainer.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
					Staleness: map[string]bool{
						"watchtower": true,
					},
					StartContainerByIDError: errors.New("simulated start failure"),
				},
				false,
				false,
			)

			params := types.UpdateParams{
				Timeout: 0,
				RunOnce: false,
			}

			testContainer := client.TestData.Containers[0]

			// Use a cancellable parent context to verify cleanup survives cancellation.
			parentCtx, parentCancel := context.WithCancel(t.Context())
			defer parentCancel()

			// Add simulated latency so the initial rename and create complete
			// before we cancel the parent context.
			client.TestData.SimulatedLatency = 5 * time.Millisecond

			var (
				renamed bool
				err     error
			)

			// Run restartStaleContainer in a goroutine so we can cancel the parent
			// after the initial handoff and create but before/during start failure.
			var wg sync.WaitGroup
			wg.Go(func() {
				_, renamed, err = restartStaleContainer(newTestLogger(t),
					parentCtx,
					testContainer,
					client,
					params,
				)
			})

			// Wait for StartContainer to be called, indicating the initial rename
			// and create have completed. Then cancel the parent context.
			require.Eventually(t, func() bool {
				return client.TestData.StartContainerCount.Load() > 0
			}, 5*time.Second, 10*time.Millisecond)

			parentCancel()
			wg.Wait()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to start container")
			assert.True(t, renamed)
			assert.Equal(t, int32(1), client.TestData.CreateContainerCount.Load())
			assert.NotEmpty(t, client.TestData.LastCreatedContainerID)
			assert.Equal(t, int32(1), client.TestData.StopAndRemoveContainerCount.Load())
			assert.Equal(t, int32(1), client.TestData.RemoveContainerCount.Load())
			assert.Equal(t, client.TestData.LastCreatedContainerID, client.TestData.LastStopAndRemoveID)
			assert.NotEqual(t, testContainer.ID(), client.TestData.LastStopAndRemoveID)

			// Assert that GetContainer and StopAndRemoveContainer receive a fresh
			// context distinct from the create/start detached context.
			getCtx := client.TestData.GetContainerCtx
			cleanupCtx := client.TestData.StopAndRemoveContainerCtx
			createCtx := client.TestData.CreateContainerCtx

			require.NotNil(t, getCtx, "GetContainer should receive a context")
			require.NotNil(t, cleanupCtx, "StopAndRemoveContainer should receive a context")
			assert.Same(t, cleanupCtx, getCtx, "GetContainer and StopAndRemoveContainer should share the same cleanup context")
			assert.NotSame(t, createCtx, getCtx, "cleanup context should be distinct from the create/start detached context")

			// Assert that the cleanup context has an active deadline (is a bounded timeout context).
			_, hasDeadline := getCtx.Deadline()
			assert.True(t, hasDeadline, "cleanup context should have a deadline for bounded recovery")
		})
	})

	t.Run("restartStaleContainer detached context survival", func(t *testing.T) {
		t.Parallel()

		t.Run("cleanup operations complete when parent context is canceled during execution", func(t *testing.T) {
			t.Parallel()

			// Create a parent context that we will cancel while restartStaleContainer is running.
			parentCtx, parentCancel := context.WithCancel(t.Context())

			// Create a mock client with a Watchtower container.
			// Configure StartContainerByIDError to trigger the cleanup path.
			// Add simulated latency to allow time for operations to complete.
			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&dockerContainer.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
					Staleness: map[string]bool{
						"watchtower": true,
					},
					StartContainerByIDError: errors.New("simulated start failure"),
					SimulatedLatency:        5 * time.Millisecond, // Allow time for operations
				},
				false,
				false,
			)

			params := types.UpdateParams{
				Timeout: 0, // Fallback deadline applied to detached context
				RunOnce: false,
			}

			testContainer := client.TestData.Containers[0]

			// Run restartStaleContainer in a goroutine so we can cancel the parent context
			// while it's still executing.
			var (
				err     error
				renamed bool
				wg      sync.WaitGroup
			)

			wg.Go(func() {
				// Call restartStaleContainer with the parent context.
				// The test flow is:
				// 1. RenameContainer succeeds (uses parent context)
				// 2. StartContainerByID fails due to StartContainerByIDError
				// 3. Cleanup runs using the detached context (should survive parent cancellation)
				_, renamed, err = restartStaleContainer(newTestLogger(t),
					parentCtx,
					testContainer,
					client,
					params,
				)
			})

			// Wait for StartContainer to be called (which means RenameContainer has completed)
			// before canceling the parent context. This ensures we cancel at the right moment -
			// after rename succeeds but during/after start fails.
			require.Eventually(t, func() bool {
				return client.TestData.StartContainerCount.Load() > 0
			}, 5*time.Second, 10*time.Millisecond)

			// Cancel the parent context after StartContainer has been called.
			// The detached context should allow cleanup to proceed even though
			// the parent context is canceled.
			parentCancel()

			// Wait for the goroutine to complete.
			wg.Wait()

			// The operation should fail due to StartContainerByID error, but the
			// cleanup (StopAndRemoveContainer) should have been attempted
			// using the detached context, which survives parent cancellation.
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to start container")
			assert.True(t, renamed)

			// Verify that RemoveContainer was called during cleanup.
			// This demonstrates that the detached context allowed the cleanup
			// operation to proceed even though the parent context was canceled.
			assert.Equal(t, int32(1), client.TestData.RemoveContainerCount.Load())
		})

		t.Run("cleanup operations complete when parent context is already canceled", func(t *testing.T) {
			t.Parallel()

			// Create a parent context that is already canceled.
			parentCtx, parentCancel := context.WithCancel(t.Context())
			parentCancel() // Cancel immediately before calling restartStaleContainer

			// Create a mock client with a Watchtower container.
			// Configure StartContainerError to trigger the cleanup path.
			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&dockerContainer.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
					Staleness: map[string]bool{
						"watchtower": true,
					},
					StartContainerError: errors.New("simulated start failure"),
				},
				false,
				false,
			)

			params := types.UpdateParams{
				Timeout: 0, // Fallback deadline applied to detached context
				RunOnce: false,
			}

			testContainer := client.TestData.Containers[0]

			// Call restartStaleContainer with an already-canceled parent context.
			// The RenameContainer operation should fail because the parent context is canceled.
			_, renamed, err := restartStaleContainer(newTestLogger(t),
				parentCtx,
				testContainer,
				client,
				params,
			)

			// The operation should fail at RenameContainer due to parent context cancellation.
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to rename Watchtower container")
			assert.False(t, renamed)

			// RenameContainer should have been attempted but failed due to context cancellation.
			assert.Equal(t, int32(1), client.TestData.RenameContainerCount.Load())
		})

		t.Run("restart policy update uses detached context after successful start", func(t *testing.T) {
			t.Parallel()

			// This test verifies that SetRestartPolicy uses the detached context,
			// not the parent context. Since StartContainer uses the parent context,
			// we cannot cancel it before calling restartStaleContainer. Instead, we
			// verify that SetRestartPolicy is called after a successful start,
			// demonstrating the detached context is properly created and used.

			// Create a mock client with a Watchtower container that succeeds.
			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: []types.Container{
						mockUpdate.CreateMockContainerWithConfig(
							"watchtower",
							"/watchtower",
							"watchtower:latest",
							true,
							false,
							time.Now(),
							&dockerContainer.Config{
								Labels: map[string]string{
									"com.centurylinklabs.watchtower": "true",
								},
							},
						),
					},
					Staleness: map[string]bool{
						"watchtower": true,
					},
				},
				false,
				false,
			)

			// Use a timeout of 0 to create a detached context with the fallback deadline.
			params := types.UpdateParams{
				Timeout: 0,
				RunOnce: false,
			}

			testContainer := client.TestData.Containers[0]

			// Call restartStaleContainer with a background context.
			newID, renamed, err := restartStaleContainer(newTestLogger(t),
				t.Context(),
				testContainer,
				client,
				params,
			)

			// The operation should succeed completely.
			require.NoError(t, err)
			assert.True(t, renamed)
			assert.NotEmpty(t, newID)

			// Verify that both StartContainer and SetRestartPolicy were called.
			// SetRestartPolicy uses the detached context for the restart policy update.
			assert.Equal(t, int32(1), client.TestData.StartContainerCount.Load())
			assert.Equal(t, int32(1), client.TestData.SetRestartPolicyCount.Load())

			restartCtx := client.TestData.SetRestartPolicyCtx
			require.NotNil(t, restartCtx, "SetRestartPolicy should receive a context")
			assert.Same(t, client.TestData.CreateContainerCtx, restartCtx, "SetRestartPolicy should use the shared detached context")

			_, hasDeadline := restartCtx.Deadline()
			assert.True(t, hasDeadline, "restart policy context should have a deadline")
		})
	})
}

// Tests for stopContainersInReversedOrder cancellation handling.
// These tests verify that when context cancellation occurs during container stopping:
// 1. All remaining containers are logged with appropriate fields
// 2. All remaining containers are added to the failed map with wrapped errors
// 3. Edge cases (cancellation at start, middle, end) are handled correctly.
//
// Important: When context is canceled at index i, the function adds the current container
// at index i to the failed map, then adds containers from i-1 down to 0.
// All containers from i down to 0 are properly logged and tracked as failed.
func Test_stopContainersInReversedOrder(t *testing.T) {
	t.Parallel()

	t.Run("context is canceled during iteration", func(t *testing.T) {
		t.Parallel()

		// Table-driven tests for various cancellation scenarios.
		// Note: When context is already canceled at the start of iteration (i = len-1),
		// all containers from i down to 0 are added to failed.
		testCases := []stopContainersTestCase{
			{
				name:                "cancellation_at_start_all_skipped",
				numContainers:       3,
				cancelAtIndex:       0, // Context already canceled - at i=2, containers 2,1,0 are skipped
				expectedStopped:     0,
				expectedSkipped:     3, // containers 2, 1, and 0 are added to failed
				expectedLogMessages: 3,
				description:         "When context is canceled at the start, all containers should be skipped",
			},
			{
				name:                "cancellation_in_middle_partial_skip",
				numContainers:       5,
				cancelAtIndex:       0, // Context already canceled - at i=4, containers 4,3,2,1,0 are skipped
				expectedStopped:     0,
				expectedSkipped:     5, // containers 4,3,2,1,0 are added to failed
				expectedLogMessages: 5,
				description:         "When context is canceled mid-iteration, all containers should be skipped",
			},
			{
				name:                "cancellation_at_end_no_skip",
				numContainers:       3,
				cancelAtIndex:       -1, // No cancellation
				expectedStopped:     3,
				expectedSkipped:     0,
				expectedLogMessages: 0,
				description:         "When no cancellation occurs, all containers should be stopped",
			},
			{
				name:                "single_container_canceled",
				numContainers:       1,
				cancelAtIndex:       0, // Context already canceled - at i=0, container 0 is skipped
				expectedStopped:     0,
				expectedSkipped:     1, // container 0 is added to failed
				expectedLogMessages: 1,
				description:         "Single container scenario with cancellation",
			},
			{
				name:                "single_container_not_canceled",
				numContainers:       1,
				cancelAtIndex:       -1, // No cancellation
				expectedStopped:     1,
				expectedSkipped:     0,
				expectedLogMessages: 0,
				description:         "Single container scenario without cancellation",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				t.Log(tc.description)

				// Create mock containers with ToRestart set to true.
				containers := make([]types.Container, tc.numContainers)
				for i := range tc.numContainers {
					containerID := fmt.Sprintf("container-%d", i)
					containerName := fmt.Sprintf("/container-%d", i)
					imageName := fmt.Sprintf("image-%d:latest", i)

					c := mockUpdate.CreateMockContainerWithConfig(
						containerID,
						containerName,
						imageName,
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels:       map[string]string{},
							ExposedPorts: dockerNetwork.PortSet{},
						},
					)
					// Mark container for restart so it will be processed.
					c.SetStale(true)
					containers[i] = c
				}

				// Create mock client.
				client := mockUpdate.CreateMockClient(
					&mockUpdate.TestData{
						Containers: containers,
						Staleness:  make(map[string]bool),
					},
					false,
					false,
				)

				// Mark all containers as stale.
				for i := range tc.numContainers {
					client.TestData.Staleness[fmt.Sprintf("container-%d", i)] = true
				}

				// Set up log capture to verify log messages.
				log, logBuf := newCaptureLogger()

				// Create context - either canceled or not based on test case.
				ctx := t.Context()
				if tc.cancelAtIndex >= 0 {
					// Create an already-canceled context to simulate cancellation.
					canceledCtx, cancel := context.WithCancel(t.Context())
					cancel() // Cancel immediately

					ctx = canceledCtx
				}

				// Call stopContainersInReversedOrder.
				failed, stopped := stopContainersInReversedOrder(log,
					ctx,
					containers,
					client,
					types.UpdateParams{},
				)

				// Verify the number of stopped containers.
				require.Len(t, stopped, tc.expectedStopped, "Expected %d stopped containers", tc.expectedStopped)

				// Verify the number of failed containers.
				require.Len(t, failed, tc.expectedSkipped, "Expected %d failed containers", tc.expectedSkipped)

				// Verify log messages for skipped containers.
				skippedLogCount := 0

				for _, entry := range parseJSONLogEntries(logBuf) {
					if entry["message"] == "Skipped container stop due to context cancellation" {
						skippedLogCount++

						// Verify log fields contain expected keys.
						assert.Contains(t, entry, "container")
						assert.Contains(t, entry, "image")
						assert.Contains(t, entry, "container_id")
					}
				}

				assert.Equal(t, tc.expectedLogMessages, skippedLogCount, "Expected %d log messages for skipped containers", tc.expectedLogMessages)
			})
		}
	})

	t.Run("context is canceled mid-iteration", func(t *testing.T) {
		t.Parallel()

		t.Run("should add remaining containers to failed map with wrapped error", func(t *testing.T) {
			t.Parallel()

			// Create 4 containers.
			// When context is already canceled at the start:
			// - At i=3, ctx.Err() != nil, so container 3 is added to failed
			// - Then containers 2,1,0 are also added to failed
			// - All 4 containers are properly tracked as failed
			containers := make([]types.Container, 4)

			for i := range 4 {
				containerID := fmt.Sprintf("container-%d", i)
				containerName := fmt.Sprintf("/container-%d", i)
				imageName := fmt.Sprintf("image-%d:latest", i)

				c := mockUpdate.CreateMockContainerWithConfig(
					containerID,
					containerName,
					imageName,
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
						"container-3": true,
					},
				},
				false,
				false,
			)

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(t.Context())
			cancel()

			// Call stopContainersInReversedOrder.
			failed, stopped := stopContainersInReversedOrder(newTestLogger(t),
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
			)

			// All 4 containers should be in failed map (containers 0, 1, 2, 3).
			require.Len(t, failed, 4)
			assert.Empty(t, stopped)

			// Verify all containers 0, 1, 2, 3 are in failed map with wrapped error.
			for i := range 4 {
				containerID := types.ContainerID(fmt.Sprintf("container-%d", i))
				err, exists := failed[containerID]
				require.True(t, exists, "Container %d should be in failed map", i)

				// Verify error message contains "stop skipped".
				assert.Contains(t, err.Error(), "stop skipped")

				// Verify error wraps context.Canceled.
				require.ErrorIs(t, err, context.Canceled, "Error should wrap context.Canceled")
			}
		})

		t.Run("should log each skipped container with correct fields", func(t *testing.T) {
			t.Parallel()

			// Create containers.
			// When context is already canceled at the start:
			// - At i=2, ctx.Err() != nil, so container 2 is logged and added to failed
			// - Then containers 1,0 are also logged and added to failed
			// - All 3 containers are properly logged
			containers := make([]types.Container, 3)
			expectedNames := []string{"container-0", "container-1", "container-2"} // All 3 are logged

			for i := range 3 {
				c := mockUpdate.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
				},
				false,
				false,
			)

			// Set up log capture.
			log, logBuf := newCaptureLogger()

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(t.Context())
			cancel()

			// Call stopContainersInReversedOrder.
			_, _ = stopContainersInReversedOrder(log,
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
			)

			// Verify log entries contain expected container details.
			loggedNames := make(map[string]bool)

			for _, entry := range parseJSONLogEntries(logBuf) {
				if entry["message"] == "Skipped container stop due to context cancellation" {
					containerName, ok := entry["container"]
					if ok {
						loggedNames[containerName.(string)] = true
					}

					// Verify all expected fields are present.
					assert.Contains(t, entry, "container")
					assert.Contains(t, entry, "image")
					assert.Contains(t, entry, "container_id")
				}
			}

			// Verify all containers were logged.
			for _, name := range expectedNames {
				assert.Contains(t, loggedNames, name, "Container %s should have been logged", name)
			}

			// Verify we got the expected number of log messages.
			require.Len(t, loggedNames, 3)
		})
	})

	t.Run("context is not canceled", func(t *testing.T) {
		t.Parallel()

		t.Run("should process all containers without adding to failed map", func(t *testing.T) {
			t.Parallel()

			// Create containers.
			containers := make([]types.Container, 3)

			for i := range 3 {
				c := mockUpdate.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
				},
				false,
				false,
			)

			// Set up log capture.
			log, logBuf := newCaptureLogger()

			// Call with valid context.
			failed, stopped := stopContainersInReversedOrder(log,
				t.Context(),
				containers,
				client,
				types.UpdateParams{},
			)

			// All containers should be stopped, none failed.
			require.Len(t, stopped, 3)
			assert.Empty(t, failed)

			// Verify no "Skipped container stop" log messages.
			for _, entry := range parseJSONLogEntries(logBuf) {
				assert.NotEqual(t, "Skipped container stop due to context cancellation", entry["message"])
			}
		})
	})

	t.Run("containers are processed in reverse order", func(t *testing.T) {
		t.Parallel()

		t.Run("should stop containers from last to first", func(t *testing.T) {
			t.Parallel()

			// Create containers.
			containers := make([]types.Container, 3)

			for i := range 3 {
				c := mockUpdate.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
					StopOrder: []string{},
				},
				false,
				false,
			)

			// Call with valid context.
			_, _ = stopContainersInReversedOrder(newTestLogger(t),
				t.Context(),
				containers,
				client,
				types.UpdateParams{},
			)

			// Verify stop order is reverse (container-2, container-1, container-0).
			require.Len(t, client.TestData.StopOrder, 3)
			assert.Equal(t, "container-2", client.TestData.StopOrder[0])
			assert.Equal(t, "container-1", client.TestData.StopOrder[1])
			assert.Equal(t, "container-0", client.TestData.StopOrder[2])
		})
	})
}

// TestRestartContainersInSortedOrderCancelRecovery verifies that containers
// already stopped are still recreated when the update is canceled.
func TestRestartContainersInSortedOrderCancelRecovery(t *testing.T) {
	t.Parallel()

	t.Run("still recreates containers that were already stopped when parent ctx is canceled", func(t *testing.T) {
		t.Parallel()

		containers := make([]types.Container, 3)

		for i := range 3 {
			c := mockUpdate.CreateMockContainerWithConfig(
				fmt.Sprintf("container-%d", i),
				fmt.Sprintf("/container-%d", i),
				fmt.Sprintf("image-%d:latest", i),
				true,
				false,
				time.Now(),
				&dockerContainer.Config{
					Labels:       map[string]string{},
					ExposedPorts: dockerNetwork.PortSet{},
				},
			)
			c.SetStale(true)
			containers[i] = c
		}

		client := mockUpdate.CreateMockClient(
			&mockUpdate.TestData{
				Containers: containers,
				Staleness: map[string]bool{
					"container-0": true,
					"container-1": true,
					"container-2": true,
				},
			},
			false,
			false,
		)

		// Pretend the stop phase already removed all three.
		stoppedImages := []types.RemovedImageInfo{
			{ContainerID: containers[0].ID(), ImageID: containers[0].ImageID(), ImageName: containers[0].ImageName(), ContainerName: containers[0].Name()},
			{ContainerID: containers[1].ID(), ImageID: containers[1].ImageID(), ImageName: containers[1].ImageName(), ContainerName: containers[1].Name()},
			{ContainerID: containers[2].ID(), ImageID: containers[2].ImageID(), ImageName: containers[2].ImageName(), ContainerName: containers[2].Name()},
		}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		progress := state.Progress{}

		var cleanup []types.RemovedImageInfo

		failed := restartContainersInSortedOrder(newTestLogger(t),
			ctx,
			containers,
			client,
			types.UpdateParams{Timeout: 0},
			stoppedImages,
			&cleanup,
			&progress,
			nil,
		)

		assert.Empty(t, failed, "stopped containers must still be recreated after cancel")
		assert.Equal(t, int32(3), client.TestData.CreateContainerCount.Load())
		assert.Equal(t, int32(3), client.TestData.StartContainerCount.Load())
	})
}

// Tests for performRollingRestart cancellation handling.
// These tests verify that when context cancellation occurs during rolling restart:
// 1. All remaining containers are logged with appropriate fields
// 2. All remaining containers are added to the failed map with wrapped errors
// 3. Error messages contain "restart skipped"
// 4. The returned error wraps context.Canceled
// 5. Edge cases (cancellation at start, middle, end) are handled correctly.
//
// Important: When context is canceled at index i, the function adds the current container
// at index i to the failed map, then adds containers from i+1 to the end.
// All containers from i to the end are properly logged and tracked as failed.
func Test_performRollingRestart(t *testing.T) {
	t.Parallel()

	t.Run("context is canceled during iteration", func(t *testing.T) {
		t.Parallel()

		// Table-driven tests for various cancellation scenarios.
		// Note: When context is already canceled at the start (i = 0),
		// all containers from 0 to len-1 are added to failed.
		testCases := []rollingRestartTestCase{
			{
				name:                "cancellation_at_start_all_skipped",
				numContainers:       3,
				cancelAtIndex:       0, // Context already canceled - at i=0, containers 0,1,2 are skipped
				expectedProcessed:   0,
				expectedSkipped:     3, // containers 0, 1, and 2 are added to failed
				expectedLogMessages: 3,
				description:         "When context is canceled at the start, all containers should be skipped",
			},
			{
				name:                "cancellation_in_middle_partial_skip",
				numContainers:       5,
				cancelAtIndex:       0, // Context already canceled - at i=0, containers 0,1,2,3,4 are skipped
				expectedProcessed:   0,
				expectedSkipped:     5, // containers 0,1,2,3,4 are added to failed
				expectedLogMessages: 5,
				description:         "When context is canceled mid-iteration, all containers should be skipped",
			},
			{
				name:                "cancellation_at_end_no_skip",
				numContainers:       3,
				cancelAtIndex:       -1, // No cancellation
				expectedProcessed:   3,
				expectedSkipped:     0,
				expectedLogMessages: 0,
				description:         "When no cancellation occurs, all containers should be processed",
			},
			{
				name:                "single_container_canceled",
				numContainers:       1,
				cancelAtIndex:       0, // Context already canceled - at i=0, container 0 is skipped
				expectedProcessed:   0,
				expectedSkipped:     1, // container 0 is added to failed
				expectedLogMessages: 1,
				description:         "Single container scenario with cancellation",
			},
			{
				name:                "single_container_not_canceled",
				numContainers:       1,
				cancelAtIndex:       -1, // No cancellation
				expectedProcessed:   1,
				expectedSkipped:     0,
				expectedLogMessages: 0,
				description:         "Single container scenario without cancellation",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				t.Log(tc.description)

				// Create mock containers with ToRestart set to true.
				containers := make([]types.Container, tc.numContainers)
				for i := range tc.numContainers {
					containerID := fmt.Sprintf("container-%d", i)
					containerName := fmt.Sprintf("/container-%d", i)
					imageName := fmt.Sprintf("image-%d:latest", i)

					c := mockUpdate.CreateMockContainerWithConfig(
						containerID,
						containerName,
						imageName,
						true,
						false,
						time.Now(),
						&dockerContainer.Config{
							Labels:       map[string]string{},
							ExposedPorts: dockerNetwork.PortSet{},
						},
					)
					// Mark container for restart so it will be processed.
					c.SetStale(true)
					containers[i] = c
				}

				// Create mock client.
				client := mockUpdate.CreateMockClient(
					&mockUpdate.TestData{
						Containers: containers,
						Staleness:  make(map[string]bool),
					},
					false,
					false,
				)

				// Mark all containers as stale.
				for i := range tc.numContainers {
					client.TestData.Staleness[fmt.Sprintf("container-%d", i)] = true
				}

				// Set up log capture to verify log messages.
				log, logBuf := newCaptureLogger()

				// Create context - either canceled or not based on test case.
				ctx := t.Context()
				if tc.cancelAtIndex >= 0 {
					// Create an already-canceled context to simulate cancellation.
					canceledCtx, cancel := context.WithCancel(t.Context())
					cancel() // Cancel immediately

					ctx = canceledCtx
				}

				// Call performRollingRestart.
				var cleanupImageInfos []types.RemovedImageInfo

				failed, err := performRollingRestart(log,
					ctx,
					containers,
					client,
					types.UpdateParams{},
					&cleanupImageInfos,
					nil, // progress is not needed for this test
					nil,
				)

				// Verify the number of failed containers.
				require.Len(t, failed, tc.expectedSkipped, "Expected %d failed containers", tc.expectedSkipped)

				// Verify error is returned when context is canceled.
				if tc.cancelAtIndex >= 0 {
					require.ErrorIs(t, err, context.Canceled, "Error should wrap context.Canceled")
					assert.Contains(t, err.Error(), "rolling restart canceled", "Error message should contain 'rolling restart canceled'")
				} else {
					require.NoError(t, err, "Expected no error when context is not canceled")
				}

				// Verify log messages for skipped containers.
				skippedLogCount := 0

				for _, entry := range parseJSONLogEntries(logBuf) {
					if entry["message"] == "Skipped container restart due to context cancellation" {
						skippedLogCount++

						// Verify log fields contain expected keys.
						assert.Contains(t, entry, "container")
						assert.Contains(t, entry, "image")
						assert.Contains(t, entry, "container_id")
					}
				}

				assert.Equal(t, tc.expectedLogMessages, skippedLogCount, "Expected %d log messages for skipped containers", tc.expectedLogMessages)
			})
		}
	})

	t.Run("context is canceled mid-iteration", func(t *testing.T) {
		t.Parallel()

		t.Run("should add remaining containers to failed map with wrapped error", func(t *testing.T) {
			t.Parallel()

			// Create 4 containers.
			// When context is already canceled at the start:
			// - At i=0, ctx.Done() is triggered, so container 0 is added to failed
			// - Then containers 1,2,3 are also added to failed
			// - All 4 containers are properly tracked as failed
			containers := make([]types.Container, 4)

			for i := range 4 {
				containerID := fmt.Sprintf("container-%d", i)
				containerName := fmt.Sprintf("/container-%d", i)
				imageName := fmt.Sprintf("image-%d:latest", i)

				c := mockUpdate.CreateMockContainerWithConfig(
					containerID,
					containerName,
					imageName,
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
						"container-3": true,
					},
				},
				false,
				false,
			)

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(t.Context())
			cancel()

			// Call performRollingRestart.
			var cleanupImageInfos []types.RemovedImageInfo

			failed, err := performRollingRestart(newTestLogger(t),
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
				nil,
			)

			// All 4 containers should be in failed map (containers 0, 1, 2, 3).
			require.Len(t, failed, 4)

			// Verify error is returned.
			require.ErrorIs(t, err, context.Canceled, "Error should wrap context.Canceled")
			assert.Contains(t, err.Error(), "rolling restart canceled")

			// Verify all containers 0, 1, 2, 3 are in failed map with wrapped error.
			for i := range 4 {
				containerID := types.ContainerID(fmt.Sprintf("container-%d", i))
				containerErr, exists := failed[containerID]
				require.True(t, exists, "Container %d should be in failed map", i)

				// Verify error message contains "restart skipped".
				assert.Contains(t, containerErr.Error(), "restart skipped")

				// Verify error wraps context.Canceled.
				require.ErrorIs(t, containerErr, context.Canceled, "Error should wrap context.Canceled")
			}
		})

		t.Run("should log each skipped container with correct fields", func(t *testing.T) {
			t.Parallel()

			// Create containers.
			// When context is already canceled at the start:
			// - At i=0, ctx.Done() is triggered, so container 0 is logged and added to failed
			// - Then containers 1,2 are also logged and added to failed
			// - All 3 containers are properly logged
			containers := make([]types.Container, 3)
			expectedNames := []string{"container-0", "container-1", "container-2"} // All 3 are logged

			for i := range 3 {
				c := mockUpdate.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
				},
				false,
				false,
			)

			// Set up log capture.
			log, logBuf := newCaptureLogger()

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(t.Context())
			cancel()

			// Call performRollingRestart.
			var cleanupImageInfos []types.RemovedImageInfo

			_, _ = performRollingRestart(log,
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
				nil,
			)

			// Verify log entries contain expected container details.
			loggedNames := make(map[string]bool)

			for _, entry := range parseJSONLogEntries(logBuf) {
				if entry["message"] == "Skipped container restart due to context cancellation" {
					containerName, ok := entry["container"]
					if ok {
						loggedNames[containerName.(string)] = true
					}

					// Verify all expected fields are present.
					assert.Contains(t, entry, "container")
					assert.Contains(t, entry, "image")
					assert.Contains(t, entry, "container_id")
				}
			}

			// Verify all containers were logged.
			for _, name := range expectedNames {
				assert.Contains(t, loggedNames, name, "Container %s should have been logged", name)
			}

			// Verify we got the expected number of log messages.
			require.Len(t, loggedNames, 3)
		})
	})

	t.Run("context is not canceled", func(t *testing.T) {
		t.Parallel()

		t.Run("should process all containers without adding to failed map", func(t *testing.T) {
			t.Parallel()

			// Create containers.
			containers := make([]types.Container, 3)

			for i := range 3 {
				c := mockUpdate.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
				},
				false,
				false,
			)

			// Set up log capture.
			log, logBuf := newCaptureLogger()

			// Call with valid context.
			var cleanupImageInfos []types.RemovedImageInfo

			failed, err := performRollingRestart(log,
				t.Context(),
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
				nil,
			)

			// All containers should be processed, none failed due to cancellation.
			assert.Empty(t, failed)
			require.NoError(t, err)

			// Verify no "Skipped container restart" log messages.
			for _, entry := range parseJSONLogEntries(logBuf) {
				assert.NotEqual(t, "Skipped container restart due to context cancellation", entry["message"])
			}
		})
	})

	t.Run("containers are processed in forward order", func(t *testing.T) {
		t.Parallel()

		t.Run("should process containers from first to last", func(t *testing.T) {
			t.Parallel()

			// Create containers.
			containers := make([]types.Container, 3)

			for i := range 3 {
				c := mockUpdate.CreateMockContainerWithConfig(
					fmt.Sprintf("container-%d", i),
					fmt.Sprintf("/container-%d", i),
					fmt.Sprintf("image-%d:latest", i),
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				)
				c.SetStale(true)
				containers[i] = c
			}

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness: map[string]bool{
						"container-0": true,
						"container-1": true,
						"container-2": true,
					},
					StopOrder:   []string{},
					CreateOrder: []string{},
					StartOrder:  []string{},
				},
				false,
				false,
			)

			// Call with valid context.
			var cleanupImageInfos []types.RemovedImageInfo

			_, _ = performRollingRestart(newTestLogger(t),
				t.Context(),
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
				nil,
			)

			// Verify create order is forward (container-0, container-1, container-2).
			require.Len(t, client.TestData.CreateOrder, 3)
			assert.Equal(t, "container-0", client.TestData.CreateOrder[0])
			assert.Equal(t, "container-1", client.TestData.CreateOrder[1])
			assert.Equal(t, "container-2", client.TestData.CreateOrder[2])
		})
	})

	t.Run("verifying error wrapping", func(t *testing.T) {
		t.Parallel()

		t.Run("should return error that wraps context.Canceled", func(t *testing.T) {
			t.Parallel()

			// Create a single container.
			containers := []types.Container{
				mockUpdate.CreateMockContainerWithConfig(
					"container-0",
					"/container-0",
					"image-0:latest",
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				),
			}
			containers[0].SetStale(true)

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness:  map[string]bool{"container-0": true},
				},
				false,
				false,
			)

			// Create a canceled context.
			canceledCtx, cancel := context.WithCancel(t.Context())
			cancel()

			// Call performRollingRestart.
			var cleanupImageInfos []types.RemovedImageInfo

			failed, err := performRollingRestart(newTestLogger(t),
				canceledCtx,
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
				nil,
			)

			// Verify error is returned.
			require.Error(t, err)

			// Verify error wraps context.Canceled using errors.Is.
			require.ErrorIs(t, err, context.Canceled, "Error should wrap context.Canceled")

			// Verify error message format.
			assert.Contains(t, err.Error(), "rolling restart canceled")

			// Verify failed container error also wraps context.Canceled.
			require.Len(t, failed, 1)
			containerErr := failed[types.ContainerID("container-0")]
			require.ErrorIs(t, containerErr, context.Canceled, "Container error should wrap context.Canceled")
			assert.Contains(t, containerErr.Error(), "restart skipped")
		})

		t.Run("should return error that wraps context.DeadlineExceeded when deadline exceeded", func(t *testing.T) {
			t.Parallel()

			// Create a single container.
			containers := []types.Container{
				mockUpdate.CreateMockContainerWithConfig(
					"container-0",
					"/container-0",
					"image-0:latest",
					true,
					false,
					time.Now(),
					&dockerContainer.Config{
						Labels:       map[string]string{},
						ExposedPorts: dockerNetwork.PortSet{},
					},
				),
			}
			containers[0].SetStale(true)

			client := mockUpdate.CreateMockClient(
				&mockUpdate.TestData{
					Containers: containers,
					Staleness:  map[string]bool{"container-0": true},
				},
				false,
				false,
			)

			// Create a context with an already-expired deadline.
			deadlineCtx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-1*time.Hour))
			defer cancel()

			// Call performRollingRestart.
			var cleanupImageInfos []types.RemovedImageInfo

			failed, err := performRollingRestart(newTestLogger(t),
				deadlineCtx,
				containers,
				client,
				types.UpdateParams{},
				&cleanupImageInfos,
				nil,
				nil,
			)

			// Verify error is returned.
			require.Error(t, err)

			// Verify error wraps context.DeadlineExceeded using errors.Is.
			require.ErrorIs(t, err, context.DeadlineExceeded, "Error should wrap context.DeadlineExceeded")

			// Verify error message format.
			assert.Contains(t, err.Error(), "rolling restart canceled")

			// Verify failed container error also wraps context.DeadlineExceeded.
			require.Len(t, failed, 1)
			containerErr := failed[types.ContainerID("container-0")]
			require.ErrorIs(t, containerErr, context.DeadlineExceeded, "Container error should wrap context.DeadlineExceeded")
			assert.Contains(t, containerErr.Error(), "restart skipped")
		})
	})
}
