package routes

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/check"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/update"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func registerCheckRoute(app *fiber.App, auth fiber.Handler, opts config.Options) {
	if opts.Client == nil {
		return
	}

	checkTimeout := opts.CheckTimeout
	if checkTimeout <= 0 {
		checkTimeout = config.DefaultCheckTimeout
	}

	handler := check.New(func(ctx context.Context, images, names []string) ([]check.ContainerCheck, error) {
		params := types.UpdateParams{
			MonitorOnly:     opts.MonitorOnly,
			NoPull:          opts.NoPull,
			LabelPrecedence: opts.LabelPrecedence,
			CooldownDelay:   opts.CooldownDelay,
		}

		imageFilter := opts.FilterByImage(images, opts.Filter)
		containerFilter := update.ContainerFilter(names)
		combinedFilter := func(c types.FilterableContainer) bool {
			return imageFilter(c) && containerFilter(c.Name(), true)
		}

		return check.CheckForUpdates(ctx, opts.Client, combinedFilter, params)
	}, checkTimeout)

	app.Post(handler.Path, auth, timeout.New(handler.Handle, timeout.Config{
		Timeout: checkTimeout,
	}))
}
