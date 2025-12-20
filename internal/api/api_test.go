package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/mock"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/api"
	mockAPI "github.com/nicholas-fedor/watchtower/pkg/api/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/filters"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
	mockTypes "github.com/nicholas-fedor/watchtower/pkg/types/mocks"
)

// TestAPI runs the Ginkgo test suite for the internal API package.
func TestAPI(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Internal API Suite")
}

var _ = ginkgo.Describe("GetAPIAddr", func() {
	ginkgo.It("should format address without brackets for non-IPv6", func() {
		addr := api.GetAPIAddr("localhost", "8080")
		gomega.Expect(addr).To(gomega.Equal("localhost:8080"))
	})

	ginkgo.It("should format address with brackets for IPv6", func() {
		addr := api.GetAPIAddr("::1", "8080")
		gomega.Expect(addr).To(gomega.Equal("[::1]:8080"))
	})

	ginkgo.It("should handle empty host", func() {
		addr := api.GetAPIAddr("", "8080")
		gomega.Expect(addr).To(gomega.Equal(":8080"))
	})
})

var _ = ginkgo.Describe("SetupAndStartAPI", func() {
	var (
		cmd    *cobra.Command
		client mockActions.MockClient
	)

	ginkgo.BeforeEach(func() {
		cmd = &cobra.Command{}
		client = mockActions.CreateMockClient(&mockActions.TestData{}, false, false)
	})

	ginkgo.When("update API is enabled", func() {
		ginkgo.It("should start API server successfully", func() {
			ctx, cancel := context.WithCancel(context.Background())

			cmd.Flags().Bool("http-api-update", true, "")
			cmd.Flags().Bool("http-api-metrics", false, "")
			cmd.Flags().Bool("http-api-periodic-polls", false, "")
			cmd.Flags().String("http-api-host", "", "")
			cmd.Flags().String("http-api-port", "8080", "")
			cmd.Flags().String("http-api-token", "test-token", "")

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

			// Mock the runUpdatesWithNotifications function
			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
			}

			// Mock other required functions
			filterByImage := func(_ []string, filter types.Filter) types.Filter {
				return filter
			}
			defaultMetrics := metrics.Default
			writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

			done := make(chan bool, 1)
			errChan := make(chan error, 1)

			// Create mock HTTP server to avoid binding to real ports
			mockServer := mockAPI.NewMockHTTPServer(ginkgo.GinkgoT())
			mockServer.EXPECT().ListenAndServe().RunAndReturn(func() error {
				done <- true
				<-ctx.Done()

				return nil
			})
			mockServer.EXPECT().Shutdown(mock.Anything).Return(nil)

			go func() {
				errChan <- api.SetupAndStartAPI(
					ctx,
					"", "0", "test-token",
					true, false, false, false,
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
			}()

			// Wait for the server to start
			<-done

			// Cancel to shutdown
			cancel()

			// Wait for the function to return
			err := <-errChan

			// Should complete without error
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.When("metrics API is enabled", func() {
		ginkgo.It("should register metrics handler", func() {
			ctx, cancel := context.WithCancel(context.Background())

			cmd.Flags().Bool("http-api-update", true, "")
			cmd.Flags().Bool("http-api-metrics", true, "")
			cmd.Flags().Bool("http-api-periodic-polls", false, "")
			cmd.Flags().String("http-api-host", "", "")
			cmd.Flags().String("http-api-port", "8080", "")
			cmd.Flags().String("http-api-token", "test-token", "")

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

			// Mock functions
			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
			}
			filterByImage := func(_ []string, filter types.Filter) types.Filter {
				return filter
			}
			defaultMetrics := metrics.Default
			writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

			done := make(chan bool, 1)
			errChan := make(chan error, 1)

			// Create mock HTTP server to avoid binding to real ports
			mockServer := mockAPI.NewMockHTTPServer(ginkgo.GinkgoT())
			mockServer.EXPECT().ListenAndServe().RunAndReturn(func() error {
				done <- true
				<-ctx.Done()

				return nil
			})
			mockServer.EXPECT().Shutdown(mock.Anything).Return(nil)

			go func() {
				errChan <- api.SetupAndStartAPI(
					ctx,
					"", "0", "test-token",
					true, true, false, false,
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
			}()

			// Wait for the server to start
			<-done

			// Cancel to shutdown
			cancel()

			// Wait for the function to return
			err := <-errChan

			// Should complete without error
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

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

			runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
			}
			filterByImage := func(_ []string, filter types.Filter) types.Filter {
				return filter
			}
			defaultMetrics := metrics.Default
			writeStartupMessage := func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {}

			err := api.SetupAndStartAPI(
				ctx,
				"", "0", "test-token",
				false, false, false, false,
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
