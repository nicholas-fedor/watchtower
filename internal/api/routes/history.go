package routes

import (
	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/internal/api/handlers/history"
)

func registerHistoryRoute(app *fiber.App, auth fiber.Handler, opts Options) {
	if opts.DefaultMetrics == nil {
		return
	}

	handler := history.New(opts.Logger, opts.DefaultMetrics().GetHistory)
	app.Get(handler.Path, auth, TimeoutMiddleware(), handler.Handle)
}
