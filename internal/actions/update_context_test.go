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
	"github.com/nicholas-fedor/watchtower/internal/actions/mocks"
)

var _ = ginkgo.Describe("the update action", func() {
	ginkgo.When("handling context cancellation and timeout scenarios", func() {
		ginkgo.It("should handle context cancellation during container listing", func() {
			client := mocks.CreateMockClient(getCommonTestData(), false, false)
			// Simulate ListContainers error by setting it
			client.TestData.ListContainersError = context.Canceled

			report, cleanupImageInfos, err := actions.Update(
				context.Background(),
				client,
				actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
			)

			gomega.Expect(err).To(gomega.HaveOccurred())
			gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to list containers"))
			gomega.Expect(report).To(gomega.BeNil())
			gomega.Expect(cleanupImageInfos).To(gomega.BeEmpty())
		})

		ginkgo.It("should handle context cancellation during staleness checking", func() {
			client := mocks.CreateMockClient(getCommonTestData(), false, false)
			// Simulate IsContainerStale error
			client.TestData.IsContainerStaleError = context.Canceled

			report, cleanupImageInfos, err := actions.Update(
				context.Background(),
				client,
				actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
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

				client := mocks.CreateMockClient(testData, false, false)
				// Simulate StopContainer failure for some containers
				client.TestData.StopContainerError = context.Canceled
				client.TestData.StopContainerFailCount = 1 // Fail the first stop attempt

				report, cleanupImageInfos, err := actions.Update(
					context.Background(),
					client,
					actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
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
		client := mocks.CreateMockClient(getCommonTestData(), false, false)
		pastDeadline := time.Now().Add(-time.Second)

		ctx, cancel := context.WithDeadline(context.Background(), pastDeadline)
		defer cancel()

		report, cleanupImageInfos, err := actions.Update(
			ctx,
			client,
			actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
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

func TestUpdateAction_CancelledContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := mocks.CreateMockClient(getCommonTestData(), false, false)
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel the context immediately

		report, cleanupImageInfos, err := actions.Update(
			cancelledCtx,
			client,
			actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
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

func TestUpdateAction_CancelledContextBeforeOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create test data with one stale container
		testData := getCommonTestData()
		testData.Staleness = map[string]bool{
			"test-container-01": true,
			"test-container-02": false,
			"test-container-03": false,
		}

		client := mocks.CreateMockClient(testData, false, false)

		// Use a context that will be cancelled, but after some operations might have started
		// Since the mock doesn't actually use context, this tests the early cancellation check
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		report, cleanupImageInfos, err := actions.Update(
			cancelledCtx,
			client,
			actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
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

func TestUpdateAction_CancelledContextDependencyScenario(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Create test data with dependencies to verify cancellation handling
		testData := getCommonTestData()

		client := mocks.CreateMockClient(testData, false, false)

		// Since we can't easily simulate timeout in sorting, test with cancelled context
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()

		report, cleanupImageInfos, err := actions.Update(
			cancelledCtx,
			client,
			actions.UpdateConfig{Cleanup: true, CPUCopyMode: "auto"},
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
