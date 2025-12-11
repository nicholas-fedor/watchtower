package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/spf13/cobra"

	actionMocks "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	apiPkg "github.com/nicholas-fedor/watchtower/internal/api"
	apiMocks "github.com/nicholas-fedor/watchtower/pkg/api/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	typeMocks "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// TestAPI runs the Ginkgo test suite for the internal API package.
func TestAPI(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Internal API Suite")
}

var _ = ginkgo.Describe("GetAPIAddr", func() {
	ginkgo.It("should format address without brackets for non-IPv6", func() {
		addr := apiPkg.GetAPIAddr("localhost", "8080")
		gomega.Expect(addr).To(gomega.Equal("localhost:8080"))
	})

	ginkgo.It("should format address with brackets for IPv6", func() {
		addr := apiPkg.GetAPIAddr("::1", "8080")
		gomega.Expect(addr).To(gomega.Equal("[::1]:8080"))
	})

	ginkgo.It("should handle empty host", func() {
		addr := apiPkg.GetAPIAddr("", "8080")
		gomega.Expect(addr).To(gomega.Equal(":8080"))
	})
})

var _ = ginkgo.Describe("SetupAndStartAPI", func() {
	var (
		cmd    *cobra.Command
		client actionMocks.MockClient
	)

	ginkgo.BeforeEach(func() {
		cmd = &cobra.Command{}
		client = actionMocks.CreateMockClient(&actionMocks.TestData{}, false, false)
	})

	ginkgo.When("update API is enabled", func() {
		ginkgo.It("should start API server successfully", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			cmd.Flags().Bool("http-api-update", true, "")
			cmd.Flags().Bool("http-api-metrics", false, "")
			cmd.Flags().Bool("http-api-periodic-polls", false, "")
			cmd.Flags().String("http-api-host", "", "")
			cmd.Flags().String("http-api-port", "8080", "")
			cmd.Flags().String("http-api-token", "test-token", "")

			notifier := typeMocks.NewMockNotifier(ginkgo.GinkgoT())

			// Mock the runUpdatesWithNotifications function
			runUpdatesWithNotifications := func(_ types.Filter, _ bool, _ bool) *metrics.Metric {
				return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
			}

			// Mock other required functions
			filterByImage := func(_ []string, filter types.Filter) types.Filter {
				return filter
			}
			defaultMetrics := metrics.Default
			writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

			// Create mock HTTP server to avoid binding to real ports
			mockServer := apiMocks.NewMockHTTPServer(ginkgo.GinkgoT())
			mockServer.EXPECT().ListenAndServe().Return(nil)

			// Use a timeout context to avoid blocking indefinitely
			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer timeoutCancel()

			err := apiPkg.SetupAndStartAPI(
				timeoutCtx,
				"", "0", "test-token",
				true, false, false,
				filters.NoFilter,
				cmd,
				"test filter",
				nil,   // updateLock
				false, // cleanup
				client,
				notifier,
				"", // scope
				"v1.0.0",
				runUpdatesWithNotifications,
				filterByImage,
				defaultMetrics,
				writeStartupMessage,
				mockServer,
			)

			// Should complete without error when context times out (clean shutdown)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.When("metrics API is enabled", func() {
		ginkgo.It("should register metrics handler", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			cmd.Flags().Bool("http-api-update", true, "")
			cmd.Flags().Bool("http-api-metrics", true, "")
			cmd.Flags().Bool("http-api-periodic-polls", false, "")
			cmd.Flags().String("http-api-host", "", "")
			cmd.Flags().String("http-api-port", "8080", "")
			cmd.Flags().String("http-api-token", "test-token", "")

			notifier := typeMocks.NewMockNotifier(ginkgo.GinkgoT())

			// Mock functions
			runUpdatesWithNotifications := func(_ types.Filter, _ bool, _ bool) *metrics.Metric {
				return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
			}
			filterByImage := func(_ []string, filter types.Filter) types.Filter {
				return filter
			}
			defaultMetrics := metrics.Default
			writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

			// Create mock HTTP server to avoid binding to real ports
			mockServer := apiMocks.NewMockHTTPServer(ginkgo.GinkgoT())
			mockServer.EXPECT().ListenAndServe().Return(nil)

			// Use a timeout context to avoid blocking
			timeoutCtx, timeoutCancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer timeoutCancel()

			err := apiPkg.SetupAndStartAPI(
				timeoutCtx,
				"", "0", "test-token",
				true, true, false,
				filters.NoFilter,
				cmd,
				"test filter",
				nil,
				false,
				client,
				notifier,
				"",
				"v1.0.0",
				runUpdatesWithNotifications,
				filterByImage,
				defaultMetrics,
				writeStartupMessage,
				mockServer,
			)

			// Should complete without error when context times out (clean shutdown)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.When("no APIs are enabled", func() {
		ginkgo.It("should return without starting server", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			cmd.Flags().Bool("http-api-update", false, "")
			cmd.Flags().Bool("http-api-metrics", false, "")
			cmd.Flags().Bool("http-api-periodic-polls", false, "")
			cmd.Flags().String("http-api-host", "", "")
			cmd.Flags().String("http-api-port", "8080", "")
			cmd.Flags().String("http-api-token", "test-token", "")

			notifier := typeMocks.NewMockNotifier(ginkgo.GinkgoT())

			runUpdatesWithNotifications := func(_ types.Filter, _ bool, _ bool) *metrics.Metric {
				return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
			}
			filterByImage := func(_ []string, filter types.Filter) types.Filter {
				return filter
			}
			defaultMetrics := metrics.Default
			writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

			err := apiPkg.SetupAndStartAPI(
				ctx,
				"", "0", "test-token",
				false, false, false,
				filters.NoFilter,
				cmd,
				"test filter",
				nil,
				false,
				client,
				notifier,
				"",
				"v1.0.0",
				runUpdatesWithNotifications,
				filterByImage,
				defaultMetrics,
				writeStartupMessage,
			)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})
})
