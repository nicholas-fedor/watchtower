package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
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
// It creates a Fiber application with the middleware stack, registers the
// configured endpoints, and starts the server. When the update API is enabled
// and UnblockHTTPAPI is false (API-only mode), this call blocks until ctx is
// canceled. Otherwise the server runs in the background and this function
// returns after the listen socket is bound so scheduled updates can run
// concurrently.
//
// Parameters:
//   - ctx: Context for server lifecycle management.
//   - opts: API configuration options.
//
// Returns:
//   - error: Non-nil if route registration or server startup fails.
func SetupAndStartAPI(ctx context.Context, opts config.Options) error {
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

	app := New(
		logrus.StandardLogger(),
		opts.RateLimit,
		ProxyConfig{
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

	// Block only in API-only update mode.
	block := opts.EnableUpdateAPI && !opts.UnblockHTTPAPI

	return runServer(
		ctx,
		app,
		address,
		opts.NoStartupMessage,
		tlsCertPath,
		tlsKeyPath,
		block,
	)
}

// isCleanServerStop reports whether err is an expected result of a graceful
// shutdown (or a nil error after Listen returns cleanly).
func isCleanServerStop(err error) bool {
	if err == nil {
		return true
	}

	return errors.Is(err, http.ErrServerClosed) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// runServer starts the Fiber app and either blocks until shutdown or returns
// after a successful bind.
//
// Fiber's GracefulContext shuts the server down when ctx is canceled.
// ListenerAddrFunc signals that the listen socket is bound so callers can
// distinguish bind success from hang/failure.
//
// Parameters:
//   - ctx: Context for server lifecycle management.
//   - app: Fiber application to start.
//   - address: Address to listen on.
//   - noStartupMessage: Whether to suppress the startup message.
//   - tlsCertPath: Path to TLS certificate file, or empty for HTTP.
//   - tlsKeyPath: Path to TLS key file, or empty for HTTP.
//   - block: When true, wait until the server stops; when false, return after bind.
//
// Returns:
//   - error: Non-nil if the server fails to start or exits with an unexpected error while blocking.
func runServer(
	ctx context.Context,
	app *fiber.App,
	address string,
	noStartupMessage bool,
	tlsCertPath, tlsKeyPath string,
	block bool,
) error {
	//nolint:nilerr // Intentionally return nil: skip socket binding when context is already canceled.
	if ctx.Err() != nil {
		return nil
	}

	readyCh := make(chan struct{})

	var readyOnce sync.Once

	listenDone := make(chan error, 1)

	listenCfg := fiber.ListenConfig{
		DisableStartupMessage: noStartupMessage,
		GracefulContext:       ctx,
		ShutdownTimeout:       ShutdownGracePeriod,
		ListenerAddrFunc: func(_ net.Addr) {
			readyOnce.Do(func() {
				close(readyCh)
			})
		},
	}

	if tlsCertPath != "" && tlsKeyPath != "" {
		listenCfg.CertFile = tlsCertPath
		listenCfg.CertKeyFile = tlsKeyPath
	}

	go func() {
		listenDone <- app.Listen(address, listenCfg)
	}()

	select {
	case <-readyCh:
		// Socket bound successfully.
	case err := <-listenDone:
		if !isCleanServerStop(err) {
			logrus.WithError(err).WithField("addr", address).
				Error("HTTP server failed to start")

			return fmt.Errorf("failed to start HTTP server: %w", err)
		}

		return nil
	case <-ctx.Done():
		// Canceled before bind completed; wait briefly for Listen to exit.
		select {
		case err := <-listenDone:
			if !isCleanServerStop(err) {
				return fmt.Errorf("failed to start HTTP server: %w", err)
			}

			return nil
		case <-time.After(ShutdownGracePeriod + time.Second):
			return nil
		}
	}

	if !block {
		go func() {
			err := <-listenDone
			if !isCleanServerStop(err) {
				logrus.WithError(err).WithField("addr", address).
					Error("HTTP server stopped unexpectedly")
			}
		}()

		return nil
	}

	// Blocking mode: wait until GracefulContext cancels and Listen returns.
	err := <-listenDone
	if !isCleanServerStop(err) {
		logrus.WithError(err).WithField("addr", address).
			Error("HTTP server failed")

		return fmt.Errorf("HTTP server failed: %w", err)
	}

	return nil
}
