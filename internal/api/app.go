package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/gofiber/fiber/v3/middleware/timeout"
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

	// handlerTimeout defines the maximum duration for non-update handlers to
	// complete. This prevents slow Docker API calls from blocking connections
	// indefinitely.
	handlerTimeout = 30 * time.Second

	// defaultRateLimitPerMinute is the fallback rate limit when a
	// non-positive value is provided.
	defaultRateLimitPerMinute = 60

	// corsMaxAge is the maximum duration (in seconds) that browsers may cache
	// CORS preflight results.
	corsMaxAge = 3600
)

// ShutdownGracePeriod defines the maximum duration allowed for the server to
// shut down gracefully.
const ShutdownGracePeriod = 5 * time.Second

// ProxyConfig holds configuration for reverse proxy support.
type ProxyConfig struct {
	// TrustedProxies is a list of trusted proxy IPs/CIDRs.
	TrustedProxies []string
	// ProxyHeader is the header for the real client IP (e.g. X-Forwarded-For).
	ProxyHeader string
}

// CORSConfig holds configuration for CORS middleware.
type CORSConfig struct {
	// AllowedOrigins is a list of origins allowed to make cross-origin requests.
	// Use ["*"] to allow all origins.
	AllowedOrigins []string
	// AllowedMethods is a list of HTTP methods allowed for cross-origin requests.
	AllowedMethods []string
	// AllowedHeaders is a list of headers allowed in cross-origin requests.
	AllowedHeaders []string
}

// New creates a new Fiber-based API application with the configured middleware
// stack and lifecycle hooks.
//
// Parameters:
//   - logrusLogger: Logger for the Fiber middleware.
//   - rateLimitPerMinute: Maximum requests per minute per IP. Values <= 0 fall
//     back to defaultRateLimitPerMinute (60).
//   - proxyCfg: Reverse proxy configuration.
//   - corsCfg: CORS middleware configuration.
//
// Returns:
//   - *fiber.App: Configured Fiber application.
func New(logrusLogger *logrus.Logger, rateLimitPerMinute int, proxyCfg ProxyConfig, corsCfg CORSConfig) *fiber.App {
	rateLimit := rateLimitPerMinute
	if rateLimit <= 0 {
		rateLimit = defaultRateLimitPerMinute
	}

	fiberCfg := fiber.Config{
		BodyLimit:     bodyLimit,
		ReadTimeout:   readTimeout,
		IdleTimeout:   idleTimeout,
		StrictRouting: true,
		CaseSensitive: true,
	}

	// Configure reverse proxy support
	if len(proxyCfg.TrustedProxies) > 0 {
		fiberCfg.TrustProxy = true

		fiberCfg.TrustProxyConfig = fiber.TrustProxyConfig{
			Proxies: proxyCfg.TrustedProxies,
		}
		if proxyCfg.ProxyHeader != "" {
			fiberCfg.ProxyHeader = proxyCfg.ProxyHeader
		} else {
			fiberCfg.ProxyHeader = fiber.HeaderXForwardedFor
		}
	}

	app := fiber.New(fiberCfg)

	// Configure CORS
	corsOrigins := corsCfg.AllowedOrigins
	enableCORS := len(corsOrigins) > 0

	corsMethods := corsCfg.AllowedMethods
	if len(corsMethods) == 0 {
		corsMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}

	corsHeaders := corsCfg.AllowedHeaders
	if len(corsHeaders) == 0 {
		corsHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization"}
	}

	middlewares := []any{
		recover.New(),
		helmet.New(),
	}

	if enableCORS {
		middlewares = append(middlewares, cors.New(cors.Config{
			AllowOrigins:     corsOrigins,
			AllowMethods:     corsMethods,
			AllowHeaders:     corsHeaders,
			AllowCredentials: false,
			MaxAge:           corsMaxAge,
		}))
	}

	middlewares = append(middlewares,
		requestid.New(),
		logger.New(logger.Config{
			Stream: &logrusWriter{logger: logrusLogger},
			Format: "${status} - ${method} ${path}\n",
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

	app.Use(middlewares...)

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

// TimeoutMiddleware returns a Fiber middleware that enforces a per-request
// timeout for all wrapped handlers. This prevents slow Docker API calls from
// blocking connections indefinitely.
func TimeoutMiddleware() fiber.Handler {
	return timeout.New(func(c fiber.Ctx) error {
		return c.Next()
	}, timeout.Config{
		Timeout: handlerTimeout,
	})
}
