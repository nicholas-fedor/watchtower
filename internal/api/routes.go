package api

import (
	"context"
	"crypto/subtle"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/keyauth"
	"github.com/gofiber/fiber/v3/middleware/timeout"

	swaggo "github.com/gofiber/contrib/v3/swaggo"

	"github.com/nicholas-fedor/watchtower/internal/api/check"
	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/containers"
	"github.com/nicholas-fedor/watchtower/internal/api/containers/details"
	"github.com/nicholas-fedor/watchtower/internal/api/events"
	"github.com/nicholas-fedor/watchtower/internal/api/health"
	"github.com/nicholas-fedor/watchtower/internal/api/history"
	"github.com/nicholas-fedor/watchtower/internal/api/images"
	"github.com/nicholas-fedor/watchtower/internal/api/metrics"
	"github.com/nicholas-fedor/watchtower/internal/api/update"
	mt "github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/container"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// validateAndRegisterRoutes validates options and registers routes.
// For the update endpoint, all required function options must be non-nil.
//
// Parameters:
//   - app: Fiber application to register routes on.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
//
// Returns:
//   - error: Non-nil if update options validation fails.
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
//
// Parameters:
//   - app: Fiber application to register routes on.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerRoutes(app *fiber.App, auth fiber.Handler, opts Options) {
	if opts.EnableUpdateAPI {
		registerUpdateRoute(app, auth, opts)
	}

	if opts.EnableMetricsAPI {
		registerMetricsRoute(app, auth, opts)
	}

	if opts.EnableContainersAPI {
		registerContainersRoute(app, auth, opts)
		registerContainersDetailsRoute(app, auth, opts)
	}

	if opts.EnableCheckAPI {
		registerCheckRoute(app, auth, opts)
	}

	if opts.EnableHistoryAPI {
		registerHistoryRoute(app, auth, opts)
	}

	if opts.EnableImagesAPI {
		registerImagesRoute(app, auth, opts)
	}

	if opts.EnableConfigAPI {
		registerConfigRoute(app, auth, opts)
	}

	if opts.EnableEventsAPI {
		registerEventsRoute(app, opts)
	}

	if opts.EnableSwaggerAPI {
		app.Get("/swagger/*", swaggo.HandlerDefault)
	}
}

// registerUpdateRoute registers the POST /v1/update endpoint.
//
// The update handler is wrapped with Fiber's timeout middleware to enforce
// a maximum execution time. The timeout covers the full lifecycle: waiting
// for the concurrency lock, performing the container update scan, and
// returning results. Handlers can detect timeout via c.Context().Done().
//
// Parameters:
//   - app: Fiber application.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerUpdateRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := update.New(func(ctx context.Context, images, containers []string) *mt.Metric {
		params := types.UpdateParams{
			Cleanup:        opts.Cleanup,
			RunOnce:        false,
			MonitorOnly:    opts.MonitorOnly,
			SkipSelfUpdate: opts.SkipSelfUpdate,
		}

		imageFilter := opts.FilterByImage(images, opts.Filter)

		containerFilter := update.ContainerFilter(containers)
		combinedFilter := func(c types.FilterableContainer) bool {
			return imageFilter(c) && containerFilter(c.Name(), true)
		}

		metric := opts.RunUpdatesWithNotifications(ctx, combinedFilter, params)
		opts.DefaultMetrics().RegisterScan(metric)

		return metric
	}, opts.UpdateLock, context.Background())

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

// registerMetricsRoute registers the GET /v1/metrics endpoint and the
// GET /v1/status endpoint.
//
// Parameters:
//   - app: Fiber application.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerMetricsRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := metrics.New()
	app.Get(handler.Path, auth, handler.Handle)

	statusHandler := metrics.NewStatusHandler(opts.DefaultMetrics().GetLastScan)
	app.Get(statusHandler.Path, auth, TimeoutMiddleware(), statusHandler.Handle)
}

// registerContainersRoute registers the GET /v1/containers endpoint.
//
// Parameters:
//   - app: Fiber application.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerContainersRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := containers.New(func(ctx context.Context) ([]containers.Status, error) {
		return containers.ListContainerStatuses(ctx, opts.Client, opts.Filter)
	})
	app.Get(handler.Path, auth, TimeoutMiddleware(), handler.Handle)
}

// registerCheckRoute registers the POST /v1/check endpoint.
//
// Parameters:
//   - app: Fiber application.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerCheckRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := check.New(func(ctx context.Context, images, names []string) ([]check.ContainerCheck, error) {
		return check.CheckForUpdates(ctx, opts.Client, opts.Filter, images, names)
	})
	app.Post(handler.Path, auth, TimeoutMiddleware(), handler.Handle)
}

// registerHistoryRoute registers the GET /v1/history endpoint.
//
// Parameters:
//   - app: Fiber application.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerHistoryRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := history.New(opts.DefaultMetrics().GetHistory)
	app.Get(handler.Path, auth, TimeoutMiddleware(), handler.Handle)
}

// registerImagesRoute registers the GET /v1/images endpoint.
//
// Parameters:
//   - app: Fiber application.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerImagesRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := images.New(func(ctx context.Context) ([]images.ImageStatus, error) {
		return images.ListImageStatuses(ctx, opts.Client, opts.Filter)
	})
	app.Get(handler.Path, auth, TimeoutMiddleware(), handler.Handle)
}

// registerConfigRoute registers the GET /v1/config endpoint.
//
// Parameters:
//   - app: Fiber application.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerConfigRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := config.New(func(ctx context.Context) (config.ConfigData, error) {
		return config.ConfigData{
			MonitorOnly:       opts.MonitorOnly,
			Cleanup:           opts.Cleanup,
			NoPull:            opts.NoPull,
			NoRestart:         opts.NoRestart,
			RollingRestart:    opts.RollingRestart,
			IncludeStopped:    opts.IncludeStopped,
			IncludeRestarting: opts.IncludeRestarting,
			LifecycleHooks:    opts.LifecycleHooks,
			LabelEnable:       opts.LabelEnable,
			FilterDesc:        opts.FilterDesc,
			Scope:             opts.Scope,
		}, nil
	})
	app.Get(handler.Path, auth, handler.Handle)
}

// registerContainersDetailsRoute registers the GET /v1/containers/details endpoint.
//
// Parameters:
//   - app: Fiber application.
//   - auth: Authentication middleware handler.
//   - opts: API configuration options.
func registerContainersDetailsRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	handler := details.New(func(ctx context.Context, name, image string) ([]details.ContainerDetails, error) {
		return details.GetContainerDetails(ctx, opts.Client, opts.Filter, name, image)
	})
	app.Get(handler.Path, auth, TimeoutMiddleware(), handler.Handle)
}

// registerEventsRoute registers the GET /v1/events SSE endpoint.
// Events support both header-based auth (for programmatic clients) and
// query-parameter auth (for browser EventSource API which cannot set custom headers).
//
// Parameters:
//   - app: Fiber application.
//   - opts: API configuration options.
func registerEventsRoute(app *fiber.App, opts Options) {
	broadcaster := opts.EventBroadcaster
	if broadcaster == nil {
		broadcaster = events.NewBroadcaster()
	}

	handler := events.NewHandler(broadcaster)

	eventsToken := opts.EventsToken

	eventsAuth := keyauth.New(keyauth.Config{
		Validator: func(_ fiber.Ctx, key string) (bool, error) {
			if subtle.ConstantTimeCompare([]byte(key), []byte(eventsToken)) == 1 {
				return true, nil
			}

			return false, keyauth.ErrMissingOrMalformedAPIKey
		},
		Extractor: extractors.Chain(
			extractors.FromAuthHeader("Bearer"),
			extractors.FromQuery("access_token"),
		),
		ErrorHandler: func(c fiber.Ctx, err error) error {
			return c.Status(fiber.StatusUnauthorized).SendString(err.Error())
		},
	})

	app.Get(handler.Path, eventsAuth, handler.Handle)
}

// registerHealthChecks registers the health check endpoints on the given Fiber app.
//
// Parameters:
//   - ctx: Context used for the readiness probe timeout.
//   - app: The Fiber application to register routes on.
//   - client: The Docker client used for the readiness probe. May be nil,
//     in which case the readiness probe will report unhealthy.
func registerHealthChecks(_ context.Context, app *fiber.App, client container.Client) {
	liveness := health.NewLivenessHandler()
	readiness := health.NewReadinessHandler(client)
	startup := health.NewStartupHandler()

	app.Get(liveness.Path, liveness.Handle)
	app.Get(readiness.Path, readiness.Handle)
	app.Get(startup.Path, startup.Handle)
}
