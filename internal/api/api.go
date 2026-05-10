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

var (
	// errMissingRunUpdatesWithNotifications indicates RunUpdatesWithNotifications was not provided.
	errMissingRunUpdatesWithNotifications = errors.New("RunUpdatesWithNotifications must be provided when EnableUpdateAPI is set")
	// errMissingFilterByImage indicates FilterByImage was not provided.
	errMissingFilterByImage = errors.New("FilterByImage must be provided when EnableUpdateAPI is set")
	// errMissingDefaultMetrics indicates DefaultMetrics was not provided.
	errMissingDefaultMetrics = errors.New("DefaultMetrics must be provided when EnableUpdateAPI is set")
)

// Options holds all configuration for SetupAndStartAPI, replacing the previous
// long parameter list with a single structured argument.
type Options struct {
	// Host to bind the HTTP API to.
	Host string
	// Port for the HTTP API server.
	Port string
	// Token for HTTP API authentication.
	Token string
	// RateLimit is the maximum authentication requests per minute per IP address.
	RateLimit int
	// EnableUpdateAPI enables the HTTP update API endpoint.
	EnableUpdateAPI bool
	// EnableMetricsAPI enables the HTTP metrics API endpoint.
	EnableMetricsAPI bool
	// UnblockHTTPAPI allows periodic polling alongside the HTTP API.
	UnblockHTTPAPI bool
	// NoStartupMessage suppresses startup messages if true.
	NoStartupMessage bool
	// Filter determines which containers are targeted for updates.
	Filter types.Filter
	// Command is the cobra.Command instance representing the executed command.
	Command *cobra.Command
	// FilterDesc is a human-readable description of the applied filter.
	FilterDesc string
	// UpdateLock is a channel ensuring only one update runs at a time, shared with the scheduler.
	UpdateLock chan bool
	// Cleanup indicates whether to remove old images after updates.
	Cleanup bool
	// MonitorOnly indicates whether to run in monitor-only mode.
	MonitorOnly bool
	// SkipSelfUpdate indicates self-update will be skipped due to host-bound port conflicts.
	SkipSelfUpdate bool
	// Client is the container client for Docker operations.
	Client container.Client
	// Notifier is the notification system instance.
	Notifier types.Notifier
	// Scope is the operational scope for Watchtower.
	Scope string
	// Version string.
	Version string
	// RunUpdatesWithNotifications runs updates with notifications.
	RunUpdatesWithNotifications func(context.Context, types.Filter, types.UpdateParams) *metrics.Metric
	// FilterByImage filters by images.
	FilterByImage func([]string, types.Filter) types.Filter
	// DefaultMetrics returns the default metrics instance.
	DefaultMetrics func() *metrics.Metrics
	// WriteStartupMessage writes the startup message.
	WriteStartupMessage func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool)
}

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
//   - opts: The Options struct containing all API configuration.
//   - server: Optional HTTPServer implementation for testing; if not provided, a real http.Server is used.
//
// Returns:
//   - error: An error if the API fails to start (excluding clean shutdown), nil otherwise.
func SetupAndStartAPI(
	ctx context.Context,
	opts Options,
	server ...api.HTTPServer,
) error {
	// Get the formatted HTTP api address string.
	address := GetAPIAddr(opts.Host, opts.Port)

	// Initialize the HTTP API with the configured authentication token and address.
	var httpAPI *api.API
	if len(server) > 0 {
		httpAPI = api.New(opts.Token, address, opts.RateLimit, server[0])
	} else {
		httpAPI = api.New(opts.Token, address, opts.RateLimit)
	}

	// Register the update API endpoint if enabled, linking it to the update handler.
	if opts.EnableUpdateAPI {
		// Validate that required injected functions are non-nil before use.
		if opts.RunUpdatesWithNotifications == nil {
			return errMissingRunUpdatesWithNotifications
		}

		if opts.FilterByImage == nil {
			return errMissingFilterByImage
		}

		if opts.DefaultMetrics == nil {
			return errMissingDefaultMetrics
		}

		updateHandler := update.New(func(images []string) *metrics.Metric {
			params := types.UpdateParams{
				Cleanup:        opts.Cleanup,
				RunOnce:        false,
				MonitorOnly:    opts.MonitorOnly,
				SkipSelfUpdate: opts.SkipSelfUpdate,
			}
			metric := opts.RunUpdatesWithNotifications(ctx, opts.FilterByImage(images, opts.Filter), params)
			opts.DefaultMetrics().RegisterScan(metric)

			return metric
		}, opts.UpdateLock)
		// Use Go 1.22+ method-based routing to restrict to POST only.
		httpAPI.RegisterFunc("POST "+updateHandler.Path, updateHandler.Handle)

		if !opts.UnblockHTTPAPI {
			opts.WriteStartupMessage(
				opts.Command,
				time.Time{},
				opts.FilterDesc,
				opts.Scope,
				opts.Client,
				opts.Notifier,
				opts.Version,
				nil, // read from flags
			)
		}
	}

	// Register the metrics API endpoint if enabled, providing access to update metrics.
	if opts.EnableMetricsAPI {
		metricsHandler := metricsAPI.New()
		// Use Go 1.22+ method-based routing to restrict to GET only.
		httpAPI.RegisterHandler("GET "+metricsHandler.Path, metricsHandler.Handle)
	}

	// Warn once at startup when self-update will be skipped due to host-bound port conflicts.
	if opts.SkipSelfUpdate {
		logrus.Warn("Skipping self-update to prevent port conflict: Watchtower container has host-bound ports")
	}

	// Start the API server, logging errors unless it's a clean shutdown.
	err := httpAPI.Start(
		ctx,
		opts.EnableUpdateAPI && !opts.UnblockHTTPAPI,
		opts.NoStartupMessage,
	)
	if err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		logrus.WithError(err).Error("Failed to start API")

		return fmt.Errorf("failed to start HTTP API: %w", err)
	}

	return nil
}
