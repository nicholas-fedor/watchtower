package actions_test

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nicholas-fedor/watchtower/internal/actions"
	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

var _ = ginkgo.Describe("the update action", func() {
	ginkgo.When("handling context cancellation and timeout scenarios", func() {
		ginkgo.It("should handle context cancellation during container listing", func() {
			client := mockActions.CreateMockClient(getCommonTestData(), false, false)
			canceledCtx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel the context immediately

			report, cleanupImageInfos, err := actions.Update(
				canceledCtx,
				client,
				types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("update canceled"))
			gomega.Expect(report).To(gomega.BeNil())
			gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
		})

		ginkgo.It("should handle context cancellation during staleness checking", func() {
			client := mockActions.CreateMockClient(getCommonTestData(), false, false)
			// Simulate IsContainerStale error
			client.TestData.IsContainerStaleError = context.Canceled

			report, cleanupImageInfos, err := actions.Update(
				context.Background(),
				client,
				types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
			)

			// Update continues but marks containers as skipped due to staleness check failure
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(report).NotTo(gomega.BeNil())
			gomega.Expect(report.Skipped()).
				To(gomega.HaveLen(3))
				// All containers skipped due to error
			gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
		})

		ginkgo.It(
			"should ensure cleanup operations are attempted even with partial failures",
			func() {
				// Create test data with multiple stale containers
				testData := getCommonTestData()
				testData.Staleness = map[string]bool{
					"test-container-01": true,
					"test-container-02": true,
					"test-container-03": true,
				}

				client := mockActions.CreateMockClient(testData, false, false)
				// Simulate StopContainer failure for some containers
				client.TestData.StopContainerError = context.Canceled
				client.TestData.StopContainerFailCount = 1 // Fail the first stop attempt

				report, cleanupImageInfos, err := actions.Update(
					context.Background(),
					client,
					types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
				)

				// Should still attempt to process and return a report
				gomega.Expect(err).
					NotTo(gomega.HaveOccurred())
					// Update completes despite some failures
				gomega.Expect(report).NotTo(gomega.BeNil())
				gomega.Expect(report.Failed()).To(gomega.HaveLen(1)) // One container failed to stop
				// Cleanup should still be attempted for successful operations
				// Since all containers use the same image, only one cleanup entry is created
				gomega.Expect(cleanupImageInfos).To(gomega.HaveLen(1)) // Deduplicated by image ID
			},
		)
	})
})

func TestUpdateAction_HandleTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := mockActions.CreateMockClient(getCommonTestData(), false, false)
		pastDeadline := time.Now().Add(-time.Second)

		ctx, cancel := context.WithDeadline(context.Background(), pastDeadline)
		defer cancel()

		report, cleanupImageInfos, err := actions.Update(
			ctx,
			client,
			types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
		)

		synctest.Wait()

		if err == nil {
			t.Fatal("expected error")
		}

		if !strings.Contains(err.Error(), "update canceled") {
			t.Fatalf("expected 'update canceled', got %s", err.Error())
		}

		if report != nil {
			t.Fatal("expected nil report")
		}

		if len(cleanupImageInfos) != 0 {
			t.Fatal("expected empty cleanupImageInfos")
		}
	})
}

func TestUpdateAction_EarlyCancellationCheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := mockActions.CreateMockClient(getCommonTestData(), false, false)
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel the context immediately

		report, cleanupImageInfos, err := actions.Update(
			canceledCtx,
			client,
			types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
		)

		synctest.Wait()

		if err == nil {
			t.Fatal("expected error")
		}

		if !strings.Contains(err.Error(), "update canceled") {
			t.Fatalf("expected 'update canceled', got %s", err.Error())
		}

		if report != nil {
			t.Fatal("expected nil report")
		}

		if len(cleanupImageInfos) != 0 {
			t.Fatal("expected empty cleanupImageInfos")
		}
	})
}

func TestUpdateAction_MidOperationCancellationCheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		testData := getCommonTestData()
		// Set simulated latency to allow operations to start before cancellation
		testData.SimulatedLatency = 10 * time.Millisecond
		client := mockActions.CreateMockClientWithContext(ctx, testData, false, false)

		// Start update in a goroutine
		done := make(chan struct{})

		var (
			report types.Report
			err    error
		)

		go func() {
			defer close(done)

			report, _, err = actions.Update(
				ctx,
				client,
				types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
			)
		}()

		// Wait a bit to allow operations to start, then cancel
		time.Sleep(5 * time.Millisecond)
		cancel()

		// Wait for the update to complete
		<-done

		synctest.Wait()

		// Depending on when cancellation occurred, err might be context canceled or nil with failed operations
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("unexpected error: %s", err.Error())
		}

		// If no early cancellation, check that operations failed due to context
		if err == nil {
			if report == nil {
				t.Fatal("expected report")
			}
			// Check that some operations failed
			if len(report.Failed()) == 0 {
				t.Fatal("expected some failed operations due to cancellation")
			}
		}
	})
}

// TestUpdateAction_ContextCancellationStartContainer tests that context cancellation
// is properly handled when starting containers.
func TestUpdateAction_ContextCancellationStartContainer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create test data with stale containers (containers need to be stopped and restarted)
		testData := getCommonTestData()
		// Mark containers as stale so they will be stopped and restarted
		testData.Staleness = map[string]bool{
			"test-container-01": true,
			"test-container-02": true,
			"test-container-03": true,
		}

		// Create client with pre-canceled context
		canceledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		client := mockActions.CreateMockClientWithContext(canceledCtx, testData, false, false)

		_, _, err := actions.Update(
			canceledCtx,
			client,
			types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
		)

		synctest.Wait()

		// The update should complete with an error related to context cancellation
		if err != nil && !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "cancel") {
			t.Fatalf("expected context-related error, got: %s", err.Error())
		}
	})
}

// TestUpdateAction_ContextTimeoutDuringProcessing tests that operations respect
// context timeouts during container processing.
func TestUpdateAction_ContextTimeoutDuringProcessing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create test data with multiple containers
		testData := getCommonTestData()
		testData.Staleness = map[string]bool{
			"test-container-01": true,
			"test-container-02": true,
			"test-container-03": true,
		}

		// Create client with context that expires immediately
		shortCtx, cancel := context.WithTimeout(context.Background(), 0)
		defer cancel()

		client := mockActions.CreateMockClientWithContext(shortCtx, testData, false, false)

		// Capture start time to verify the call completes within a reasonable bound
		start := time.Now()

		report, _, err := actions.Update(
			shortCtx,
			client,
			types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
		)

		synctest.Wait()

		elapsed := time.Since(start)

		// Smoke test: ensure no panic occurs when handling zero-timeout context
		// Verify the call completed within a reasonable time bound (not hanging)
		if elapsed > 5*time.Second {
			t.Fatalf("Update call took too long (%v), expected to complete quickly with zero-timeout context", elapsed)
		}

		// Assert that at least one of the outputs is non-nil
		if err == nil && report == nil {
			t.Fatalf("Expected either error or report to be non-nil, got err=%v and report=%v", err, report)
		}
	})
}

// TestUpdateAction_ErrorPropagationContextErrors tests that errors from client operations
// are properly propagated through the update process.
func TestUpdateAction_ErrorPropagationContextErrors(t *testing.T) {
	tests := []struct {
		name                 string
		errorToReturn        error
		containerStaleness   map[string]bool
		expectedErrorPattern string
	}{
		{
			name:                 "ListContainers context error",
			errorToReturn:        context.Canceled,
			containerStaleness:   nil,
			expectedErrorPattern: "update canceled",
		},
		{
			name:                 "StopContainer context error",
			errorToReturn:        context.DeadlineExceeded,
			containerStaleness:   map[string]bool{"test-container-01": true, "test-container-02": true, "test-container-03": true},
			expectedErrorPattern: "", // May not produce error, but should handle gracefully
		},
		{
			name:                 "StartContainer context error",
			errorToReturn:        context.Canceled,
			containerStaleness:   map[string]bool{"test-container-01": true, "test-container-02": true, "test-container-03": true},
			expectedErrorPattern: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testData := getCommonTestData()
				testData.Staleness = tc.containerStaleness

				client := mockActions.CreateMockClient(testData, false, false)

				// Set the error to return based on test case
				switch {
				case strings.Contains(tc.name, "ListContainers"):
					client.TestData.ListContainersError = tc.errorToReturn
				case strings.Contains(tc.name, "StopContainer"):
					client.TestData.StopContainerError = tc.errorToReturn
				case strings.Contains(tc.name, "StartContainer"):
					client.TestData.StartContainerError = tc.errorToReturn
				}

				report, cleanupImageInfos, err := actions.Update(
					context.Background(),
					client,
					types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
				)

				synctest.Wait()

				if tc.expectedErrorPattern != "" && err != nil {
					if !strings.Contains(err.Error(), tc.expectedErrorPattern) &&
						!strings.Contains(err.Error(), "context") {
						t.Fatalf("expected error containing '%s' or 'context', got: %s",
							tc.expectedErrorPattern, err.Error())
					}
				}

				// For non-cancellation errors, update should complete with report
				if err == nil && report != nil {
					// Update completed, check that cleanup was attempted
					if len(tc.containerStaleness) > 0 {
						// Some containers should have been processed
						totalProcessed := len(report.Updated()) + len(report.Failed()) + len(report.Skipped())
						if totalProcessed != len(tc.containerStaleness) {
							t.Fatalf("expected %d processed containers, got %d (updated: %d, failed: %d, skipped: %d)",
								len(tc.containerStaleness), totalProcessed, len(report.Updated()), len(report.Failed()), len(report.Skipped()))
						}
					}
				}

				_ = cleanupImageInfos
			})
		})
	}
}

// TestUpdateAction_ContextEdgeCases tests edge cases with context handling.
func TestUpdateAction_ContextEdgeCases(t *testing.T) {
	tests := []struct {
		name             string
		contextSetup     func() (context.Context, context.CancelFunc)
		staleContainers  map[string]bool
		simulatedLatency time.Duration // Optional latency for context timeout testing
	}{
		{
			name: "Background context should work",
			contextSetup: func() (context.Context, context.CancelFunc) {
				return context.Background(), func() {}
			},
			staleContainers: map[string]bool{
				"test-container-01": true,
				"test-container-02": true,
				"test-container-03": true,
			},
		},
		{
			name: "WithCancel context immediately canceled",
			contextSetup: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx, func() {}
			},
			staleContainers: map[string]bool{
				"test-container-01": true,
				"test-container-02": true,
				"test-container-03": true,
			},
		},
		{
			name: "WithTimeout already expired",
			contextSetup: func() (context.Context, context.CancelFunc) {
				// Use a past deadline to simulate immediate timeout
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
			},
			staleContainers: map[string]bool{
				"test-container-01": true,
				"test-container-02": true,
				"test-container-03": true,
			},
		},
		{
			name: "WithTimeout short timeout",
			contextSetup: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 1*time.Millisecond)
			},
			staleContainers: map[string]bool{
				"test-container-01": true,
				"test-container-02": true,
				"test-container-03": true,
			},
			// Set simulated latency to allow timeout to expire during operations
			simulatedLatency: 5 * time.Millisecond,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				testData := getCommonTestData()
				testData.Staleness = tc.staleContainers
				// Apply simulated latency if specified (for context timeout testing)
				if tc.simulatedLatency > 0 {
					testData.SimulatedLatency = tc.simulatedLatency
				}

				ctx, cancel := tc.contextSetup()
				defer cancel()

				client := mockActions.CreateMockClientWithContext(ctx, testData, false, false)

				report, cleanupImageInfos, err := actions.Update(
					ctx,
					client,
					types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
				)

				synctest.Wait()

				// For background context, update should succeed
				if tc.name == "Background context should work" {
					if err != nil {
						t.Fatalf("Background context should not produce error: %s", err.Error())
					}

					if report == nil {
						t.Fatal("Expected report for successful update")
					}
				}

				// For background context, error is nil (already asserted above)
				// For canceled/expired contexts, error is expected
				if tc.name != "Background context should work" {
					if err == nil {
						t.Fatalf("Expected error for %s context, but got nil", tc.name)
					}

					// Verify the error message contains context-related keywords
					if !strings.Contains(err.Error(), "context") &&
						!strings.Contains(err.Error(), "cancel") &&
						!strings.Contains(err.Error(), "deadline") &&
						!strings.Contains(err.Error(), "timeout") &&
						!strings.Contains(err.Error(), "update canceled") {
						t.Fatalf("expected error to contain context-related keyword, got: %s", err)
					}
				}

				// Keep cleanupImageInfos for potential cleanup assertions
				_ = cleanupImageInfos
			})
		})
	}
}
