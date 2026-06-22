package api

import (
	"crypto/sha256"
	"crypto/subtle"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/keyauth"
	"github.com/sirupsen/logrus"
)

func newAPIAuthMiddleware(token string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).SendString("API token not configured")
		}

		expectedHash := sha256.Sum256([]byte(token))

		extracted, err := extractors.FromAuthHeader("Bearer").Extract(c)
		if err != nil {
			extracted, err = extractors.FromCookie("access_token").Extract(c)
			if err != nil {
				logrus.WithField("ip", c.IP()).Warn("Missing or malformed API key")

				return c.Status(fiber.StatusUnauthorized).SendString(keyauth.ErrMissingOrMalformedAPIKey.Error())
			}
		}

		providedHash := sha256.Sum256([]byte(extracted))
		if subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) != 1 {
			logrus.WithField("ip", c.IP()).Warn("Invalid token attempt")

			return c.Status(fiber.StatusUnauthorized).SendString(keyauth.ErrMissingOrMalformedAPIKey.Error())
		}

		return c.Next()
	}
}
