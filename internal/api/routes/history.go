package routes

import (
	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/history"
)

func registerHistoryRoute(app *fiber.App, auth fiber.Handler, opts config.Options) {
	handler := history.New(opts.DefaultMetrics().GetHistory)
	app.Get(handler.Path, auth, config.TimeoutMiddleware(), handler.Handle)
}
