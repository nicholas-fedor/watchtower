package routes

import (
	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/settings"
)

func registerSettingsRoute(app *fiber.App, auth fiber.Handler, opts config.Options) {
	if opts.Settings == nil {
		return
	}

	// In API-only update mode the scheduler never starts, so the schedule has no effect.
	scheduleActive := !opts.EnableUpdateAPI || opts.UnblockHTTPAPI

	handler := settings.New(opts.Logger, opts.Settings, scheduleActive)
	app.Get(handler.Path, auth, config.TimeoutMiddleware(), handler.Get)
	app.Patch(handler.Path, auth, config.TimeoutMiddleware(), handler.Patch)
}
