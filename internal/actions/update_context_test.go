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
			// Simulate ListContainers error by setting it
			client.TestData.ListContainersError = context.Canceled

			report, cleanupImageInfos, err := actions.Update(
				context.Background(),
				client,
				types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to list containers"))
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

		if !strings.Contains(err.Error(), "update cancelled") {
			t.Fatalf("expected 'update cancelled', got %s", err.Error())
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
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel the context immediately

		report, cleanupImageInfos, err := actions.Update(
			cancelledCtx,
			client,
			types.UpdateParams{Cleanup: true, CPUCopyMode: "auto"},
		)

		synctest.Wait()

		if err == nil {
			t.Fatal("expected error")
		}

		if !strings.Contains(err.Error(), "update cancelled") {
			t.Fatalf("expected 'update cancelled', got %s", err.Error())
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
		client := mockActions.CreateMockClientWithContext(ctx, getCommonTestData(), false, false)

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
