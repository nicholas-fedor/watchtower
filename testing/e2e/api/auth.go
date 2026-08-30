package api

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// bearerPrefix is the Authorization scheme serve expects.
const bearerPrefix = "Bearer "

// authMiddleware rejects requests that lack the configured bearer token.
//
// Health, OpenAPI, the dashboard, and static assets stay public so an
// operator can open a browser on loopback without a header.
//
// Parameters:
//   - token: Expected bearer value. Empty disables auth.
//
// Returns:
//   - fiber.Handler: Middleware.
func authMiddleware(token string) fiber.Handler {
	return func(c fiber.Ctx) error {
		if token == "" {
			return c.Next()
		}

		if publicPath(c.Path()) {
			return c.Next()
		}

		got, ok := strings.CutPrefix(c.Get("Authorization"), bearerPrefix)
		if !ok || got != token {
			return c.Status(http.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}

		return c.Next()
	}
}

// publicPath reports routes that skip bearer auth.
//
// Parameters:
//   - path: Request path.
//
// Returns:
//   - bool: True when the path is public.
func publicPath(path string) bool {
	switch path {
	case "/v1/health", "/v1/docs", "/v1/openapi.json", "/v1/openapi.yaml", "/":
		return true
	}

	return strings.HasPrefix(path, "/static") || strings.HasPrefix(path, "/runs")
}
