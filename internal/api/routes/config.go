package routes

import (
	"context"

	"github.com/gofiber/fiber/v3"

	apiconfig "github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/config"
)

func registerConfigRoute(app *fiber.App, auth fiber.Handler, opts apiconfig.Options) {
	handler := config.New(func(_ context.Context) (config.ConfigData, error) {
		return config.ConfigData{
			MonitorOnly:       opts.MonitorOnly,
			Cleanup:           opts.Cleanup,
			NoPull:            opts.NoPull,
			NoRestart:         opts.NoRestart,
			RollingRestart:    opts.RollingRestart,
			IncludeStopped:    opts.IncludeStopped,
			IncludeRestarting: opts.IncludeRestarting,
			LifecycleHooks:    opts.LifecycleHooks,
			LabelEnable:       opts.LabelEnable,
			FilterDesc:        opts.FilterDesc,
			Scope:             opts.Scope,
		}, nil
	})
	app.Get(handler.Path, auth, handler.Handle)
}
