package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/routes"
)

// Server defines the interface for Watchtower's HTTP API server.
type Server interface {
	Listen(addr string, config ...fiber.ListenConfig) error
	ShutdownWithTimeout(timeout time.Duration) error
}

// Compile-time check that *fiber.App implements Server.
var _ Server = (*fiber.App)(nil)

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
	if port == "" {
		return host
	}

	address := host + ":" + port

	bracketed := strings.Contains(host, ":") && !strings.HasPrefix(host, "[")
	if bracketed {
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
func SetupAndStartAPI(ctx context.Context, opts config.Options) error {
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

	shouldRequireToken := opts.EnableUpdateAPI ||
		opts.EnableMetricsAPI ||
		opts.EnableContainersAPI ||
		opts.EnableCheckAPI ||
		opts.EnableSwaggerAPI ||
		opts.EnableHistoryAPI ||
		opts.EnableImagesAPI ||
		opts.EnableConfigAPI

	shouldRequireEventsToken := opts.EnableEventsAPI

	if shouldRequireToken && opts.Token == "" {
		return config.ErrMissingAPIToken
	}

	if shouldRequireEventsToken && opts.EventsToken == "" {
		return config.ErrMissingEventsAPIToken
	}

	app := New(logrus.StandardLogger(), opts.RateLimit, ProxyConfig{
		TrustedProxies: opts.TrustedProxies,
		ProxyHeader:    opts.ProxyHeader,
	}, CORSConfig{
		AllowedOrigins: opts.CORSAllowedOrigins,
	})

	authMiddleware := NewAPIAuthMiddleware(opts.Token)

	err := routes.ValidateAndRegister(ctx, app, authMiddleware, opts)
	if err != nil {
		return fmt.Errorf("route registration failed: %w", err)
	}

	if opts.SkipSelfUpdate {
		logrus.Warn("Skipping self-update to prevent port conflict: Watchtower container has host-bound ports")
	}

	tlsCertPath, tlsKeyPath := opts.TLSCertPath, opts.TLSKeyPath

	if (tlsCertPath == "") != (tlsKeyPath == "") {
		return config.ErrMissingTLSConfig
	}

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
	//nolint:nilerr // Intentionally return nil: skip socket binding when context is already canceled.
	if ctx.Err() != nil {
		return nil
	}

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
		err := app.ShutdownWithTimeout(ShutdownGracePeriod)
		if err != nil && !errors.Is(err, context.Canceled) {
			logrus.WithError(err).Debug("Failed to shut down HTTP server")

			return fmt.Errorf("server shutdown failed: %w", err)
		}

		return nil
	}
}
