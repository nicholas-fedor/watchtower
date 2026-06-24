package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/api/events"
	_ "github.com/nicholas-fedor/watchtower/internal/api/swagger"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	mt "github.com/nicholas-fedor/watchtower/pkg/metrics"
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

// Server defines the interface for Watchtower's HTTP API server.
type Server interface {
	Listen(addr string, config ...fiber.ListenConfig) error
	ShutdownWithTimeout(timeout time.Duration) error
}

// Compile-time check that *fiber.App implements Server.
var _ Server = (*fiber.App)(nil)

// Options holds all configuration for SetupAndStartAPI.
type Options struct {
	Host                        string
	Port                        string
	Token                       string
	RateLimit                   int
	EnableUpdateAPI             bool
	EnableMetricsAPI            bool
	EnableContainersAPI         bool
	EnableCheckAPI              bool
	EnableSwaggerAPI            bool
	EnableHealthAPI             bool
	EnableHistoryAPI            bool
	EnableImagesAPI             bool
	EnableConfigAPI             bool
	EnableEventsAPI             bool
	EnableFullAPI               bool
	UnblockHTTPAPI              bool
	NoStartupMessage            bool
	TLSCertPath                 string
	TLSKeyPath                  string
	CORSAllowedOrigins          []string
	TrustedProxies              []string
	ProxyHeader                 string
	Filter                      types.Filter
	Command                     *cobra.Command
	FilterDesc                  string
	UpdateLock                  chan bool
	Cleanup                     bool
	MonitorOnly                 bool
	NoPull                      bool
	NoRestart                   bool
	RollingRestart              bool
	IncludeStopped              bool
	IncludeRestarting           bool
	LifecycleHooks              bool
	LabelEnable                 bool
	SkipSelfUpdate              bool
	Client                      container.Client
	Notifier                    types.Notifier
	Scope                       string
	Version                     string
	RunUpdatesWithNotifications func(context.Context, types.Filter, types.UpdateParams) *mt.Metric
	FilterByImage               func([]string, types.Filter) types.Filter
	DefaultMetrics              func() *mt.Metrics
	WriteStartupMessage         func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool)
	EventBroadcaster            *events.Broadcaster
}

// GetAPIAddr formats the API address string from host and port, bracketing
// IPv6 addresses as needed.
//
// Parameters:
//   - host: Hostname or IP address.
//   - port: Port number.
//
// Returns:
//   - string: Formatted address string.
func GetAPIAddr(host, port string) string {
	address := host + ":" + port
	if strings.Contains(host, ":") && net.ParseIP(host) != nil {
		address = "[" + host + "]:" + port
	}

	return address
}

// SetupAndStartAPI configures and launches the HTTP API.
//
// It creates a Fiber application with the middleware stack, registers health
// checks, registers the configured endpoints, and starts the server in a
// background goroutine. The server runs until ctx is canceled, then
// gracefully shuts down.
//
// Parameters:
//   - ctx: Context for server lifecycle management.
//   - opts: API configuration options.
//
// Returns:
//   - error: Non-nil if route registration or server startup fails.
func SetupAndStartAPI(ctx context.Context, opts Options) error {
	if opts.EnableFullAPI {
		opts.EnableUpdateAPI = true
		opts.EnableCheckAPI = true
		opts.EnableMetricsAPI = true
		opts.EnableContainersAPI = true
		opts.EnableSwaggerAPI = true
		opts.EnableHealthAPI = true
		opts.EnableHistoryAPI = true
		opts.EnableImagesAPI = true
		opts.EnableConfigAPI = true
		opts.EnableEventsAPI = true
	}

	if !opts.EnableUpdateAPI &&
		!opts.EnableMetricsAPI &&
		!opts.EnableContainersAPI &&
		!opts.EnableCheckAPI &&
		!opts.EnableSwaggerAPI &&
		!opts.EnableHealthAPI &&
		!opts.EnableHistoryAPI &&
		!opts.EnableImagesAPI &&
		!opts.EnableConfigAPI &&
		!opts.EnableEventsAPI {
		return nil
	}

	address := GetAPIAddr(opts.Host, opts.Port)

	if opts.Token == "" {
		logrus.WithField("addr", address).Fatal("API token is empty or unset")
	}

	app := New(logrus.StandardLogger(), opts.RateLimit, ProxyConfig{
		TrustedProxies: opts.TrustedProxies,
		ProxyHeader:    opts.ProxyHeader,
	}, CORSConfig{
		AllowedOrigins: opts.CORSAllowedOrigins,
	})

	authMiddleware := newAPIAuthMiddleware(opts.Token)

	if opts.EnableHealthAPI {
		registerHealthChecks(ctx, app, opts.Client)
	}

	err := validateAndRegisterRoutes(app, authMiddleware, opts)
	if err != nil {
		return err
	}

	if opts.SkipSelfUpdate {
		logrus.Warn("Skipping self-update to prevent port conflict: Watchtower container has host-bound ports")
	}

	tlsCertPath, tlsKeyPath := opts.TLSCertPath, opts.TLSKeyPath

	return runServer(ctx, app, address, opts.NoStartupMessage, tlsCertPath, tlsKeyPath)
}

// runServer starts the Fiber app in a background goroutine and blocks until
// ctx is canceled, then gracefully shuts down.
//
// Parameters:
//   - ctx: Context for server lifecycle management.
//   - app: Fiber application to start.
//   - address: Address to listen on.
//   - noStartupMessage: Whether to suppress the startup message.
//   - tlsCertPath: Path to TLS certificate file, or empty for HTTP.
//   - tlsKeyPath: Path to TLS key file, or empty for HTTP.
//
// Returns:
//   - error: Non-nil if server shutdown fails.
func runServer(ctx context.Context, app *fiber.App, address string, noStartupMessage bool, tlsCertPath, tlsKeyPath string) error {
	listenErrCh := make(chan error, 1)

	go func() {
		var err error
		if tlsCertPath != "" && tlsKeyPath != "" {
			err = app.Listen(address, fiber.ListenConfig{
				DisableStartupMessage: noStartupMessage,
				CertFile:              tlsCertPath,
				CertKeyFile:           tlsKeyPath,
			})
		} else {
			err = app.Listen(address, fiber.ListenConfig{
				DisableStartupMessage: noStartupMessage,
			})
		}

		if err != nil {
			logrus.WithError(err).WithField("addr", address).
				Error("HTTP server failed to start")

			listenErrCh <- err
		}
	}()

	select {
	case err := <-listenErrCh:
		return fmt.Errorf("failed to start HTTP server: %w", err)
	case <-ctx.Done():
	}

	err := app.ShutdownWithTimeout(ShutdownGracePeriod)
	if err != nil && !errors.Is(err, context.Canceled) {
		logrus.WithError(err).Debug("Failed to shut down HTTP server")

		return fmt.Errorf("server shutdown failed: %w", err)
	}

	return nil
}

// validateUpdateOptions validates that all required update options are set.
//
// Parameters:
//   - opts: API configuration options to validate.
//
// Returns:
//   - error: Non-nil if any required option is missing.
func validateUpdateOptions(opts Options) error {
	if opts.RunUpdatesWithNotifications == nil {
		return errMissingRunUpdatesWithNotifications
	}

	if opts.FilterByImage == nil {
		return errMissingFilterByImage
	}

	if opts.DefaultMetrics == nil {
		return errMissingDefaultMetrics
	}

	return nil
}
