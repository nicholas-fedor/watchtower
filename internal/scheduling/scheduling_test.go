package scheduling_test

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/spf13/cobra"

	actionMocks "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/scheduling"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// TestScheduling runs the Ginkgo test suite for the internal scheduling package.
func TestScheduling(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Internal Scheduling Suite")
}

var _ = ginkgo.Describe("WaitForRunningUpdate", func() {
	ginkgo.It("should return immediately when no update is running", func() {
		ctx := context.Background()
		lock := make(chan bool, 1)
		lock <- true // lock is available

		start := time.Now()
		scheduling.WaitForRunningUpdate(ctx, lock)
		elapsed := time.Since(start)

		gomega.Expect(elapsed).To(gomega.BeNumerically("<", 10*time.Millisecond))
	})

	ginkgo.It("should wait for running update to complete", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		lock := make(chan bool, 1) // lock is taken (no value in channel)

		start := time.Now()
		scheduling.WaitForRunningUpdate(ctx, lock)
		elapsed := time.Since(start)

		// Should have waited for the timeout
		gomega.Expect(elapsed).To(gomega.BeNumerically(">=", 40*time.Millisecond))
	})
})

var _ = ginkgo.Describe("RunUpgradesOnSchedule", func() {
	var (
		cmd    *cobra.Command
		client actionMocks.MockClient
	)

	ginkgo.BeforeEach(func() {
		cmd = &cobra.Command{}
		client = actionMocks.CreateMockClient(&actionMocks.TestData{}, false, false)
	})

	ginkgo.It("should handle empty schedule spec", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd.Flags().Bool("update-on-start", false, "")

		runUpdatesWithNotifications := func(_ types.Filter, _ bool) *metrics.Metric {
			return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
		}

		writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string) {}

		// Use timeout to avoid hanging
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer timeoutCancel()

		err := scheduling.RunUpgradesOnSchedule(
			timeoutCtx,
			cmd,
			filters.NoFilter,
			"test filter",
			nil,   // no lock
			false, // cleanup
			"",    // empty schedule
			writeStartupMessage,
			runUpdatesWithNotifications,
			client,
			"",  // scope
			nil, // no notifier
			"v1.0.0",
			false, // updateOnStart
		)

		// Should complete without error when context times out (clean cancellation)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.It("should handle invalid cron spec", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		runUpdatesWithNotifications := func(_ types.Filter, _ bool) *metrics.Metric {
			return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
		}

		writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string) {}

		err := scheduling.RunUpgradesOnSchedule(
			ctx,
			cmd,
			filters.NoFilter,
			"test filter",
			nil,
			false,
			"invalid cron spec",
			writeStartupMessage,
			runUpdatesWithNotifications,
			client,
			"",
			nil,
			"v1.0.0",
			false, // updateOnStart
		)

		gomega.Expect(err).To(gomega.HaveOccurred())
		gomega.Expect(err.Error()).To(gomega.ContainSubstring("failed to schedule updates"))
	})

	ginkgo.It("should trigger update on start when enabled", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cmd.PersistentFlags().Bool("update-on-start", true, "")

		updateCalled := false
		runUpdatesWithNotifications := func(_ types.Filter, _ bool) *metrics.Metric {
			updateCalled = true

			return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
		}

		writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string) {}

		// Use timeout to avoid hanging
		timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 10*time.Millisecond)
		defer timeoutCancel()

		err := scheduling.RunUpgradesOnSchedule(
			timeoutCtx,
			cmd,
			filters.NoFilter,
			"test filter",
			nil,
			false,
			"", // no schedule
			writeStartupMessage,
			runUpdatesWithNotifications,
			client,
			"",
			nil,
			"v1.0.0",
			true, // updateOnStart
		)

		gomega.Expect(err).NotTo(gomega.HaveOccurred()) // clean timeout
		gomega.Expect(updateCalled).To(gomega.BeTrue())
	})

	ginkgo.It("should handle context cancellation", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		runUpdatesWithNotifications := func(_ types.Filter, _ bool) *metrics.Metric {
			return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
		}

		writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string) {}

		// Cancel immediately
		cancelledCtx, cancelFunc := context.WithCancel(ctx)
		cancelFunc()

		err := scheduling.RunUpgradesOnSchedule(
			cancelledCtx,
			cmd,
			filters.NoFilter,
			"test filter",
			nil,
			false,
			"",
			writeStartupMessage,
			runUpdatesWithNotifications,
			client,
			"",
			nil,
			"v1.0.0",
			false, // updateOnStart
		)

		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})
})
