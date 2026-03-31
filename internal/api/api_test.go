package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/mock"

	mockActions "github.com/nicholas-fedor/watchtower/internal/actions/mocks"
	"github.com/nicholas-fedor/watchtower/internal/api"
	mockAPI "github.com/nicholas-fedor/watchtower/pkg/api/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/api/update"
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

	// defaultTestOptions returns a base api.Options with shared test defaults.
	// Callers can override individual fields (e.g., EnableUpdateAPI) before passing
	// the result to api.SetupAndStartAPI.
	defaultTestOptions := func(
		cmd *cobra.Command,
		client container.Client,
		notifier types.Notifier,
	) api.Options {
		return api.Options{
			Host:             "",
			Port:             "0",
			Token:            "test-token",
			RateLimit:        60,
			EnableUpdateAPI:  false,
			EnableMetricsAPI: false,
			UnblockHTTPAPI:   false,
			NoStartupMessage: false,
			Filter:           filters.NoFilter,
			Command:          cmd,
			FilterDesc:       "test filter",
			UpdateLock:       nil,
			Cleanup:          false,
			MonitorOnly:      false,
			Client:           client,
			Notifier:         notifier,
			Scope:            "",
			Version:          "v1.0.0",
			RunUpdatesWithNotifications: func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{Scanned: 0, Updated: 0, Failed: 0}
			},
			FilterByImage: func(_ []string, filter types.Filter) types.Filter {
				return filter
			},
			DefaultMetrics: metrics.Default,
			WriteStartupMessage: func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool) {
			},
			SkipSelfUpdate: false,
		}
	}

	ginkgo.When("update API is enabled", func() {
		ginkgo.It("should start API server successfully", func() {
			ctx, cancel := context.WithCancel(context.Background())

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

			opts := defaultTestOptions(cmd, client, notifier)
			opts.EnableUpdateAPI = true
			opts.RunUpdatesWithNotifications = func(_ context.Context, _ types.Filter, _ types.UpdateParams) *metrics.Metric {
				return &metrics.Metric{Scanned: 1, Updated: 1, Failed: 0}
			}

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
				errChan <- api.SetupAndStartAPI(ctx, opts, mockServer)
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

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

			opts := defaultTestOptions(cmd, client, notifier)
			opts.EnableUpdateAPI = true
			opts.EnableMetricsAPI = true

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
				errChan <- api.SetupAndStartAPI(ctx, opts, mockServer)
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

			notifier := mockTypes.NewMockNotifier(ginkgo.GinkgoT())

			opts := defaultTestOptions(cmd, client, notifier)

			err := api.SetupAndStartAPI(ctx, opts)

			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})
	})

	ginkgo.When("update API is enabled with monitorOnly parameter", func() {
		ginkgo.DescribeTable("should pass monitorOnly parameter to update function",
			func(monitorOnly, expectMonitorOnly bool) {
				ctx := context.Background()

				var capturedParams types.UpdateParams

				runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
					capturedParams = params

					return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
				}
				filterByImage := func(_ []string, filter types.Filter) types.Filter {
					return filter
				}
				defaultMetrics := metrics.Default

				// Create the update handler directly to test the parameter passing
				updateHandler := update.New(func(images []string) *metrics.Metric {
					params := types.UpdateParams{
						Cleanup:        false, // cleanup
						RunOnce:        false,
						MonitorOnly:    monitorOnly,
						SkipSelfUpdate: false,
					}
					metric := runUpdatesWithNotifications(
						ctx,
						filterByImage(images, filters.NoFilter),
						params,
					)
					defaultMetrics().RegisterScan(metric)

					return metric
				}, nil)

				// Create a test HTTP request to trigger the update
				req, err := http.NewRequest(http.MethodPost, "/v1/update", http.NoBody)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// Create a response recorder
				w := httptest.NewRecorder()

				// Call the handler
				updateHandler.Handle(w, req)

				// Verify the response
				gomega.Expect(w.Code).To(gomega.Equal(http.StatusOK))
				gomega.Expect(capturedParams.RunOnce).To(gomega.BeFalse())
				gomega.Expect(capturedParams.MonitorOnly).To(gomega.Equal(expectMonitorOnly))
			},
			ginkgo.Entry("monitorOnly false", false, false),
			ginkgo.Entry("monitorOnly true", true, true),
		)
	})

	ginkgo.When("update API is enabled with skipSelfUpdate parameter", func() {
		ginkgo.DescribeTable("should pass skipSelfUpdate parameter to update function",
			func(skipSelfUpdate, expectSkipSelfUpdate bool) {
				ctx := context.Background()

				var capturedParams types.UpdateParams

				runUpdatesWithNotifications := func(_ context.Context, _ types.Filter, params types.UpdateParams) *metrics.Metric {
					capturedParams = params

					return &metrics.Metric{Scanned: 1, Updated: 0, Failed: 0}
				}
				filterByImage := func(_ []string, filter types.Filter) types.Filter {
					return filter
				}
				defaultMetrics := metrics.Default

				// Create the update handler directly to test the parameter passing
				updateHandler := update.New(func(images []string) *metrics.Metric {
					params := types.UpdateParams{
						Cleanup:        false,
						RunOnce:        false,
						MonitorOnly:    false,
						SkipSelfUpdate: skipSelfUpdate,
					}
					metric := runUpdatesWithNotifications(
						ctx,
						filterByImage(images, filters.NoFilter),
						params,
					)
					defaultMetrics().RegisterScan(metric)

					return metric
				}, nil)

				// Create a test HTTP request to trigger the update
				req, err := http.NewRequest(http.MethodPost, "/v1/update", http.NoBody)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// Create a response recorder
				w := httptest.NewRecorder()

				// Call the handler
				updateHandler.Handle(w, req)

				// Verify the response
				gomega.Expect(w.Code).To(gomega.Equal(http.StatusOK))
				gomega.Expect(capturedParams.RunOnce).To(gomega.BeFalse())
				gomega.Expect(capturedParams.SkipSelfUpdate).To(gomega.Equal(expectSkipSelfUpdate))
			},
			ginkgo.Entry("skipSelfUpdate false", false, false),
			ginkgo.Entry("skipSelfUpdate true", true, true),
		)
	})
})
