package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/sirupsen/logrus"
)

const (
	// bodyLimit defines the maximum request body size (1 MiB).
	bodyLimit = 1 << 20

	// readTimeout defines the maximum duration for reading the entire request,
	// including the body.
	readTimeout = 10 * time.Second

	// idleTimeout defines the maximum amount of time to wait for the next
	// request when keep-alives are enabled.
	idleTimeout = 30 * time.Second

	// updateHandlerTimeout defines the maximum duration for the update handler
	// to complete. This covers the full lifecycle: waiting for the lock,
	// performing the update scan, and returning results.
	updateHandlerTimeout = 10 * time.Minute

	// defaultRateLimitPerMinute is the fallback rate limit when a
	// non-positive value is provided.
	defaultRateLimitPerMinute = 60
)

// ShutdownGracePeriod defines the maximum duration allowed for the server to
// shut down gracefully.
const ShutdownGracePeriod = 5 * time.Second

// New creates a new Fiber-based API application with the configured middleware
// stack and lifecycle hooks.
//
// The rateLimitPerMinute parameter sets the maximum requests per minute per IP.
// It is clamped to defaultRateLimitPerMinute if non-positive.
//
// Per-handler timeouts (e.g., for the update endpoint) are applied at the
// route level via Fiber's timeout middleware, not at the app config level.
func New(logrusLogger *logrus.Logger, rateLimitPerMinute int) *fiber.App {
	rateLimit := rateLimitPerMinute
	if rateLimit <= 0 {
		rateLimit = defaultRateLimitPerMinute
	}

	app := fiber.New(fiber.Config{
		BodyLimit:     bodyLimit,
		ReadTimeout:   readTimeout,
		IdleTimeout:   idleTimeout,
		StrictRouting: true,
		CaseSensitive: true,
	})

	app.Use(
		recover.New(),
		helmet.New(),
		requestid.New(),
		logger.New(logger.Config{
			Stream: &logrusWriter{logger: logrusLogger},
			Format: "${time} ${requestid} ${latency} ${status} - ${method} ${path}\n",
		}),
		compress.New(compress.Config{
			Level: compress.LevelBestSpeed,
		}),
		limiter.New(limiter.Config{
			Max:               rateLimit,
			Expiration:        time.Minute,
			LimiterMiddleware: limiter.SlidingWindow{},
			KeyGenerator:      func(c fiber.Ctx) string { return c.IP() },
			LimitReached: func(c fiber.Ctx) error {
				logrus.WithField("ip", c.IP()).Warn("Rate limit exceeded")

				return c.SendStatus(fiber.StatusTooManyRequests)
			},
		}),
	)

	app.Hooks().OnListen(func(data fiber.ListenData) error {
		logrus.WithField("addr", data.Host+":"+data.Port).
			Info("Starting HTTP API server")

		return nil
	})

	app.Hooks().OnPreShutdown(func() error {
		logrus.Info("Initiating HTTP API shutdown")

		return nil
	})

	app.Hooks().OnPostShutdown(func(err error) error {
		if err != nil {
			logrus.WithError(err).Warn("HTTP server shut down with error")
		} else {
			logrus.Info("HTTP server shut down successfully")
		}

		return nil
	})

	return app
}
