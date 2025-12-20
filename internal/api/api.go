// Package api provides application-specific HTTP API orchestration for Watchtower, coordinating the setup and management of API endpoints with business logic integration.
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/pkg/api"
	metricsAPI "github.com/nicholas-fedor/watchtower/pkg/api/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/api/update"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// GetAPIAddr formats the API address string based on host and port.
func GetAPIAddr(host, port string) string {
	address := host + ":" + port
	if host != "" && strings.Contains(host, ":") && net.ParseIP(host) != nil {
		address = "[" + host + "]:" + port
	}

	return address
}

// SetupAndStartAPI configures and launches the HTTP API if enabled by configuration flags.
//
// It sets up update and metrics endpoints, starts the API server in blocking or non-blocking mode,
// and handles startup errors, ensuring the API integrates seamlessly with Watchtower's update workflow.
//
// Parameters:
//   - ctx: The context controlling the API's lifecycle, enabling graceful shutdown on cancellation.
//   - apiHost: The host to bind the HTTP API to.
//   - apiPort: The port for the HTTP API server.
//   - apiToken: The authentication token for HTTP API access.
//   - enableUpdateAPI: Enables the HTTP update API endpoint.
//   - enableMetricsAPI: Enables the HTTP metrics API endpoint.
//   - unblockHTTPAPI: Allows periodic polling alongside the HTTP API.
//   - noStartupMessage: Suppresses startup messages if true.
//   - filter: The types.Filter determining which containers are targeted for updates.
//   - command: The cobra.Command instance representing the executed command.
//   - filterDesc: A human-readable description of the applied filter.
//   - updateLock: A channel ensuring only one update runs at a time, shared with the scheduler.
//   - cleanup: Boolean indicating whether to remove old images after updates.
//   - client: Container client for Docker operations.
//   - notifier: Notification system instance.
//   - scope: Operational scope for Watchtower.
//   - version: Version string.
//   - runUpdatesWithNotifications: Function to run updates with notifications.
//   - filterByImage: Function to filter by images.
//   - defaultMetrics: Function to get default metrics.
//   - writeStartupMessage: Function to write startup message.
//
// Returns:
//   - error: An error if the API fails to start (excluding clean shutdown), nil otherwise.
func SetupAndStartAPI(
	ctx context.Context,
	apiHost, apiPort, apiToken string,
	enableUpdateAPI, enableMetricsAPI, unblockHTTPAPI, noStartupMessage bool,
	filter types.Filter,
	command *cobra.Command,
	filterDesc string,
	updateLock chan bool,
	cleanup bool,
	client container.Client,
	notifier types.Notifier,
	scope string,
	version string,
	runUpdatesWithNotifications func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric,
	filterByImage func([]string, types.Filter) types.Filter,
	defaultMetrics func() *metrics.Metrics,
	writeStartupMessage func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool),
	server ...api.HTTPServer,
) error {
	// Get the formatted HTTP api address string.
	address := GetAPIAddr(apiHost, apiPort)

	// Initialize the HTTP API with the configured authentication token and address.
	var httpAPI *api.API
	if len(server) > 0 {
		httpAPI = api.New(apiToken, address, server[0])
	} else {
		httpAPI = api.New(apiToken, address)
	}

	// Register the update API endpoint if enabled, linking it to the update handler.
	if enableUpdateAPI {
		updateHandler := update.New(func(images []string) *metrics.Metric {
			params := types.UpdateParams{
				Cleanup:       cleanup,
				RunOnce:       true,
				SkipSelfUpdate: false, // SkipWatchtowerSelfUpdate is not needed for API-triggered updates
			}
			metric := runUpdatesWithNotifications(ctx, filterByImage(images, filter), params)
			defaultMetrics().RegisterScan(metric)

			return metric
		}, updateLock)
		httpAPI.RegisterFunc(updateHandler.Path, updateHandler.Handle)

		if !unblockHTTPAPI {
			writeStartupMessage(
				command,
				time.Time{},
				filterDesc,
				scope,
				client,
				notifier,
				version,
				nil, // read from flags
			)
		}
	}

	// Register the metrics API endpoint if enabled, providing access to update metrics.
	if enableMetricsAPI {
		metricsHandler := metricsAPI.New()
		httpAPI.RegisterHandler(metricsHandler.Path, metricsHandler.Handle)
	}

	// Start the API server, logging errors unless it's a clean shutdown.
	if err := httpAPI.Start(ctx, enableUpdateAPI && !unblockHTTPAPI, noStartupMessage); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		logrus.WithError(err).Error("Failed to start API")

		return fmt.Errorf("failed to start HTTP API: %w", err)
	}

	return nil
}
