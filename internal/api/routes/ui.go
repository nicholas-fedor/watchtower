package routes

import (
	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/ui"
)

// registerUIRoute mounts the built-in web UI under /ui/ without API auth.
// The page signs in through /ui/session, which sets the access_token cookie
// that the protected /v1/* routes accept.
//
// Parameters:
//   - app: Fiber application.
//   - opts: API configuration options.
func registerUIRoute(app *fiber.App, opts config.Options) {
	handler, err := ui.New(opts.Logger, ui.Options{
		Token:   opts.Token,
		Version: opts.Version,
		Features: ui.Features{
			Containers: opts.EnableContainersAPI,
			Check:      opts.EnableCheckAPI,
			Update:     opts.EnableUpdateAPI,
			History:    opts.EnableHistoryAPI,
			Config:     opts.EnableConfigAPI,
			Settings:   opts.EnableSettingsAPI,
			Images:     opts.EnableImagesAPI,
			Metrics:    opts.EnableMetricsAPI,
			Swagger:    opts.EnableSwaggerAPI,
		},
	})
	if err != nil {
		// The files are compiled into the binary, so this only fails on a broken build.
		if opts.Logger != nil {
			opts.Logger.Error().Err(err).Msg("Web UI is unavailable")
		}

		return
	}

	app.Get("/", handler.Redirect)
	app.Get("/ui", handler.Redirect)
	app.Get(ui.BasePath, handler.Index)
	app.Get(ui.AssetsPath, handler.Asset)
	app.Get(ui.SessionPath, handler.Session)
	app.Post(ui.SessionPath, handler.SignIn)
	app.Delete(ui.SessionPath, handler.SignOut)
}
