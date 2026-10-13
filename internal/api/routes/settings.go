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

	handler := settings.NewWithNotifier(opts.Logger, opts.Settings, scheduleActive, opts.Notifier)
	app.Get(handler.Path, auth, config.TimeoutMiddleware(), handler.Get)
	app.Patch(handler.Path, auth, config.TimeoutMiddleware(), handler.Patch)
	app.Post(handler.Path+"/test-notification", auth, config.TimeoutMiddleware(), handler.TestNotification)
	app.Post("/v1/notify/test", auth, config.TimeoutMiddleware(), handler.TestNotification)
}
