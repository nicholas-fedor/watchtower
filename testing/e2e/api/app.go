package api

import (
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"

	"github.com/nicholas-fedor/watchtower/testing/e2e/control"
)

const (
	// bodyLimit is the maximum JSON request body (1 MiB).
	bodyLimit = 1 << 20
	// readTimeout is how long Fiber waits to read a request.
	readTimeout = 15 * time.Second
	// idleTimeout is the keep-alive idle budget.
	idleTimeout = 60 * time.Second
)

// NewApp builds a Fiber app with Huma operations mounted at /v1.
//
// Parameters:
//   - svc: Control-plane service.
//   - token: Optional bearer token. Empty disables auth.
//
// Returns:
//   - *fiber.App: Configured application.
//   - huma.API: Huma handle for tests.
func NewApp(svc *control.Service, token string) (*fiber.App, huma.API) {
	app := fiber.New(fiber.Config{
		AppName:      "watchtower-e2e",
		ReadTimeout:  readTimeout,
		IdleTimeout:  idleTimeout,
		BodyLimit:    bodyLimit,
		ErrorHandler: fiberError,
	})
	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(authMiddleware(token))

	cfg := huma.DefaultConfig("Watchtower e2e", "1.0.0")
	cfg.OpenAPIPath = "/v1/openapi"
	cfg.DocsPath = "/v1/docs"

	api := humafiber.New(app, cfg)
	registerRuns(api, svc)
	registerCases(api, svc)
	registerOps(api, svc)
	registerEvents(api, svc)

	return app, api
}
