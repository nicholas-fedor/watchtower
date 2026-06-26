package routes

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/check"
)

func registerCheckRoute(app *fiber.App, auth fiber.Handler, opts config.Options) {
	handler := check.New(func(ctx context.Context, images, names []string) ([]check.ContainerCheck, error) {
		return check.CheckForUpdates(ctx, opts.Client, opts.Filter, images, names)
	})
	app.Post(handler.Path, auth, config.TimeoutMiddleware(), handler.Handle)
}
