package routes

import (
	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/internal/api/auth"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/events"
)

func registerEventsRoute(app *fiber.App, opts Options) {
	handler := events.NewHandler(opts.Logger, opts.EventBroadcaster, opts.CORSAllowedOrigins)

	app.Get(handler.Path, auth.NewEventsMiddleware(opts.Logger, opts.EventsToken), handler.Handle())
}
