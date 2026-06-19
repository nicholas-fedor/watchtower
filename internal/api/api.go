package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/healthcheck"
	"github.com/gofiber/fiber/v3/middleware/keyauth"
	"github.com/gofiber/fiber/v3/middleware/timeout"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/nicholas-fedor/watchtower/internal/api/containers"
	"github.com/nicholas-fedor/watchtower/internal/api/metrics"
	"github.com/nicholas-fedor/watchtower/internal/api/update"
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
//
// It abstracts the underlying Fiber application to allow dependency injection
// for testing with mock implementations.
type Server interface {
	// Listen starts the HTTP server on the given address.
	Listen(addr string, config ...fiber.ListenConfig) error
	// Shutdown gracefully shuts down the server with a timeout.
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
	UnblockHTTPAPI              bool
	NoStartupMessage            bool
	Filter                      types.Filter
	Command                     *cobra.Command
	FilterDesc                  string
	UpdateLock                  chan bool
	Cleanup                     bool
	MonitorOnly                 bool
	SkipSelfUpdate              bool
	Client                      container.Client
	Notifier                    types.Notifier
	Scope                       string
	Version                     string
	RunUpdatesWithNotifications func(context.Context, types.Filter, types.UpdateParams) *mt.Metric
	FilterByImage               func([]string, types.Filter) types.Filter
	DefaultMetrics              func() *mt.Metrics
	WriteStartupMessage         func(*cobra.Command, time.Time, string, string, container.Client, types.Notifier, string, *bool)
}

// GetAPIAddr formats the API address string based on host and port.
func GetAPIAddr(host, port string) string {
	address := host + ":" + port
	if strings.Contains(host, ":") && net.ParseIP(host) != nil {
		address = "[" + host + "]:" + port
	}

	return address
}

// SetupAndStartAPI configures and launches the HTTP API if enabled by
// configuration flags.
//
// It creates a Fiber application with the middleware stack, registers health
// checks, registers the configured endpoints, and starts the server in a
// background goroutine. The server runs until ctx is canceled, then
// gracefully shuts down.
func SetupAndStartAPI(ctx context.Context, opts Options) error {
	if !opts.EnableUpdateAPI && !opts.EnableMetricsAPI && !opts.EnableContainersAPI {
		return nil
	}

	address := GetAPIAddr(opts.Host, opts.Port)

	if opts.Token == "" {
		logrus.WithField("addr", address).Fatal("API token is empty or unset")
	}

	tokenHash := sha256.Sum256([]byte(opts.Token))

	app := New(logrus.StandardLogger(), opts.RateLimit)
	authMiddleware := newKeyAuthMiddleware(tokenHash)

	registerHealthChecks(ctx, app, opts.Client)

	err := validateAndRegisterRoutes(app, authMiddleware, opts)
	if err != nil {
		return err
	}

	if opts.SkipSelfUpdate {
		logrus.Warn("Skipping self-update to prevent port conflict: Watchtower container has host-bound ports")
	}

	return runServer(ctx, app, address, opts.NoStartupMessage)
}

// registerHealthChecks registers liveness, readiness, and startup probe
// endpoints using Fiber's healthcheck middleware.
//
// The endpoints follow the Kubernetes health probe convention:
//   - /livez: Liveness probe — always returns 200 OK when the server is running.
//   - /readyz: Readiness probe — verifies Docker client connectivity by calling
//     ListContainers. Returns 200 OK if the client is connected, 503 otherwise.
//   - /startupz: Startup probe — always returns 200 OK once the server has started.
//
// All health endpoints are registered unconditionally (independent of which
// /v1/ APIs are enabled) and require no authentication.
//
// Parameters:
//   - ctx: The context used for the readiness probe check, allowing the probe
//     to observe server shutdown. Should be the server lifecycle context.
//   - app: The Fiber application to register routes on.
//   - client: The Docker client used for the readiness probe. May be nil,
//     in which case the readiness probe will report unhealthy.
func registerHealthChecks(ctx context.Context, app *fiber.App, client container.Client) {
	// Liveness: the server is running.
	app.Get(healthcheck.LivenessEndpoint, healthcheck.New())

	// Readiness: the Docker client is connected and responsive.
	app.Get(healthcheck.ReadinessEndpoint, healthcheck.New(healthcheck.Config{
		Probe: func(c fiber.Ctx) bool {
			if client == nil {
				return false
			}

			probeCtx, cancel := context.WithTimeout(ctx, readinessProbeTimeout)
			defer cancel()

			_, err := client.ListContainers(probeCtx)

			return err == nil
		},
	}))

	// Startup: alias to liveness; the server has started.
	app.Get(healthcheck.StartupEndpoint, healthcheck.New())
}

const readinessProbeTimeout = 5 * time.Second

// newKeyAuthMiddleware creates a Fiber KeyAuth middleware that validates
// Bearer tokens using SHA-256 hashing and constant-time comparison to prevent
// timing side-channel attacks.
//
// The token is extracted from the Authorization header (Bearer scheme per
// RFC 7235), hashed with SHA-256, and compared against the expected hash
// using crypto/subtle.ConstantTimeCompare.
//
// Returns 401 Unauthorized for missing or invalid tokens.
func newKeyAuthMiddleware(expectedHash [sha256.Size]byte) fiber.Handler {
	return keyauth.New(keyauth.Config{
		Validator: func(c fiber.Ctx, key string) (bool, error) {
			providedHash := sha256.Sum256([]byte(key))
			if subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) == 1 {
				return true, nil
			}

			return false, keyauth.ErrMissingOrMalformedAPIKey
		},
		ErrorHandler: func(c fiber.Ctx, err error) error {
			logrus.WithField("ip", c.IP()).Warn("Invalid token attempt")

			return c.Status(fiber.StatusUnauthorized).SendString(err.Error())
		},
	})
}

// validateAndRegisterRoutes validates options and registers routes.
// For the update endpoint, all required function options must be non-nil.
func validateAndRegisterRoutes(app *fiber.App, auth fiber.Handler, opts Options) error {
	if opts.EnableUpdateAPI {
		err := validateUpdateOptions(opts)
		if err != nil {
			return err
		}
	}

	registerRoutes(app, auth, opts)

	return nil
}

// registerRoutes registers all enabled API endpoints on the given Fiber app.
func registerRoutes(app *fiber.App, auth fiber.Handler, opts Options) {
	if opts.EnableUpdateAPI {
		registerUpdateRoute(app, auth, opts)
	}

	if opts.EnableMetricsAPI {
		registerMetricsRoute(app, auth)
	}

	if opts.EnableContainersAPI {
		registerContainersRoute(app, auth, opts)
	}
}

// registerUpdateRoute registers the POST /v1/update endpoint.
//
// The update handler is wrapped with Fiber's timeout middleware to enforce
// a maximum execution time. The timeout covers the full lifecycle: waiting
// for the concurrency lock, performing the container update scan, and
// returning results. Handlers can detect timeout via c.Context().Done().
func registerUpdateRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := update.New(func(ctx context.Context, images []string) *mt.Metric {
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

	app.Post(handler.Path, auth, timeout.New(handler.Handle, timeout.Config{
		Timeout: updateHandlerTimeout,
	}))

	if !opts.UnblockHTTPAPI {
		opts.WriteStartupMessage(
			opts.Command, time.Time{}, opts.FilterDesc, opts.Scope,
			opts.Client, opts.Notifier, opts.Version, nil,
		)
	}
}

// registerMetricsRoute registers the GET /v1/metrics endpoint.
func registerMetricsRoute(app *fiber.App, auth fiber.Handler) {
	handler := metrics.New()
	app.Get(handler.Path, auth, handler.Handle)
}

// registerContainersRoute registers the GET /v1/containers endpoint.
func registerContainersRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := containers.New(func(ctx context.Context) ([]containers.Status, error) {
		return containers.ListContainerStatuses(ctx, opts.Client, opts.Filter)
	})
	app.Get(handler.Path, auth, handler.Handle)
}

// runServer starts the Fiber app in a background goroutine and blocks until
// ctx is canceled, then gracefully shuts down.
func runServer(ctx context.Context, app *fiber.App, address string, noStartupMessage bool) error {
	go func() {
		err := app.Listen(address, fiber.ListenConfig{
			DisableStartupMessage: noStartupMessage,
		})
		if err != nil {
			logrus.WithError(err).WithField("addr", address).
				Debug("HTTP server encountered an error")
		}
	}()

	<-ctx.Done()

	err := app.ShutdownWithTimeout(ShutdownGracePeriod)
	if err != nil && !errors.Is(err, context.Canceled) {
		logrus.WithError(err).Debug("Failed to shut down HTTP server")

		return fmt.Errorf("server shutdown failed: %w", err)
	}

	return nil
}

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
