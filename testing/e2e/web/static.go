package web

import (
	_ "embed"

	"github.com/gofiber/fiber/v3"
)

// htmxJS is htmx 4.0.0, vendored so the dashboard does not depend on a CDN.
//
//go:embed static/htmx.min.js
var htmxJS []byte

// appCSS is the compiled Tailwind stylesheet (make css).
//
//go:embed static/app.css
var appCSS []byte

// mountStatic serves vendored htmx and dashboard CSS.
//
// Parameters:
//   - app: Fiber application.
func mountStatic(app *fiber.App) {
	app.Get("/static/htmx.min.js", serveHTMX)
	app.Get("/static/app.css", serveCSS)
}

// serveHTMX writes the vendored htmx 4.0.0 script.
//
// Parameters:
//   - c: Fiber request context.
//
// Returns:
//   - error: Write failure.
func serveHTMX(c fiber.Ctx) error {
	c.Set("Content-Type", "application/javascript; charset=utf-8")
	c.Set("Cache-Control", "public, max-age=31536000, immutable")

	return c.Send(htmxJS)
}

// serveCSS writes the dashboard stylesheet.
//
// Parameters:
//   - c: Fiber request context.
//
// Returns:
//   - error: Write failure.
func serveCSS(c fiber.Ctx) error {
	c.Set("Content-Type", "text/css; charset=utf-8")
	c.Set("Cache-Control", "no-cache")

	return c.Send(appCSS)
}
