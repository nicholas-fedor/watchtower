package routes

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/keyauth"

	"github.com/nicholas-fedor/watchtower/internal/api/config"
	"github.com/nicholas-fedor/watchtower/internal/api/handlers/events"
)

func registerEventsRoute(app *fiber.App, opts config.Options) {
	broadcaster := opts.EventBroadcaster
	if broadcaster == nil {
		broadcaster = events.NewBroadcaster()
	}

	handler := events.NewHandler(broadcaster, opts.CORSAllowedOrigins)

	eventsToken := opts.EventsToken

	eventsAuth := keyauth.New(keyauth.Config{
		Validator: func(_ fiber.Ctx, key string) (bool, error) {
			if subtle.ConstantTimeCompare([]byte(key), []byte(eventsToken)) == 1 {
				return true, nil
			}

			return false, keyauth.ErrMissingOrMalformedAPIKey
		},
		Extractor: extractors.Chain(
			extractors.FromAuthHeader("Bearer"),
			extractors.FromQuery("access_token"),
		),
		ErrorHandler: func(c fiber.Ctx, err error) error {
			return c.Status(fiber.StatusUnauthorized).SendString(err.Error())
		},
	})

	app.Get(handler.Path, eventsAuth, handler.Handle())
}
