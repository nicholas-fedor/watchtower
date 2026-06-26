package routes

import (
	"context"

	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/check"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func registerCheckRoute(app *fiber.App, auth fiber.Handler, opts config.Options) {
	checkParams := types.UpdateParams{
		MonitorOnly:     opts.MonitorOnly,
		NoPull:          opts.NoPull,
		LabelPrecedence: opts.LabelPrecedence,
		CooldownDelay:   opts.CooldownDelay,
	}
	handler := check.New(func(ctx context.Context, images, names []string) ([]check.ContainerCheck, error) {
		return check.CheckForUpdates(ctx, opts.Client, opts.Filter, images, names, checkParams)
	})
	app.Post(handler.Path, auth, config.TimeoutMiddleware(), handler.Handle)
}
