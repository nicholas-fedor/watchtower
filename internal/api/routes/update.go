package routes

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/update"
	mt "github.com/nicholas-fedor/watchtower/internal/metrics"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	updateHandlerTimeout = 10 * time.Minute
)

func registerUpdateRoute(app *fiber.App, auth fiber.Handler, opts config.Options) {
	handler := update.New(func(ctx context.Context, images, containers []string) *mt.Metric {
		params := types.UpdateParams{
			Cleanup:        opts.Cleanup,
			RunOnce:        false,
			MonitorOnly:    opts.MonitorOnly,
			SkipSelfUpdate: opts.SkipSelfUpdate,
		}

		imageFilter := opts.FilterByImage(images, opts.Filter)

		containerFilter := update.ContainerFilter(containers)
		combinedFilter := func(c types.FilterableContainer) bool {
			return imageFilter(c) && containerFilter(c.Name(), true)
		}

		metric := opts.RunUpdatesWithNotifications(ctx, combinedFilter, params)
		opts.DefaultMetrics().RegisterScan(metric)

		return metric
	}, opts.UpdateLock, context.Background())

	app.Post(handler.Path, auth, timeout.New(handler.Handle, timeout.Config{
		Timeout: updateHandlerTimeout,
	}))

	if !opts.UnblockHTTPAPI {
		opts.WriteStartupMessage(
			opts.Command, time.Time{}, opts.FilterDesc, opts.Scope,
			opts.Client, opts.Notifier, opts.Version, nil,
		)
	}
}
