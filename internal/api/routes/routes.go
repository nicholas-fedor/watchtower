package routes

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v3"

	swaggo "github.com/gofiber/contrib/v3/swaggo"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	_ "github.com/nicholas-fedor/watchtower/internal/api/swagger"
)

func ValidateAndRegister(ctx context.Context, app *fiber.App, auth fiber.Handler, opts config.Options) error {
	if opts.EnableUpdateAPI {
		err := config.ValidateUpdateOptions(opts)
		if err != nil {
			return fmt.Errorf("update options validation failed: %w", err)
		}
	}

	Register(ctx, app, auth, opts)

	return nil
}

func Register(ctx context.Context, app *fiber.App, auth fiber.Handler, opts config.Options) {
	if opts.EnableHealthAPI {
		registerHealthRoute(app, opts)
	}

	if opts.EnableUpdateAPI {
		registerUpdateRoute(ctx, app, auth, opts)
	}

	if opts.EnableMetricsAPI {
		registerMetricsRoute(app, auth, opts)
	}

	if opts.EnableContainersAPI {
		registerContainersRoute(app, auth, opts)
		registerContainersDetailsRoute(app, auth, opts)
	}

	if opts.EnableCheckAPI {
		registerCheckRoute(app, auth, opts)
	}

	if opts.EnableHistoryAPI {
		registerHistoryRoute(app, auth, opts)
	}

	if opts.EnableImagesAPI {
		registerImagesRoute(app, auth, opts)
	}

	if opts.EnableConfigAPI {
		registerConfigRoute(app, auth, opts)
	}

	if opts.EnableEventsAPI {
		registerEventsRoute(app, opts)
	}

	if opts.EnableSwaggerAPI {
		app.Get("/swagger/*", swaggo.HandlerDefault)
	}
}
