package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/keyauth"
	"github.com/rs/zerolog"
)

// accessTokenKey names the cookie and query parameter that carry a token.
const accessTokenKey = "access_token"

// NewAPIMiddleware returns a Fiber middleware that validates the HTTP API
// token.
//
// Accepted credentials (first match wins):
//   - Authorization: Bearer <token>
//   - Authorization: <token> (raw value. Swagger UI apiKey style)
//   - Cookie access_token=<token>
//
// Parameters:
//   - log: Logger for failed attempts. Nil discards them.
//   - token: Configured API token. Empty rejects every request.
//
// Returns:
//   - fiber.Handler: Authentication middleware.
func NewAPIMiddleware(log *zerolog.Logger, token string) fiber.Handler {
	return newMiddleware(log, token, "API token not configured", extractors.FromCookie(accessTokenKey))
}

// NewEventsMiddleware returns a Fiber middleware that validates the events
// token for the server-sent events stream.
//
// Accepted credentials (first match wins):
//   - Authorization: Bearer <token>
//   - Authorization: <token> (raw value. Swagger UI apiKey style)
//   - Query parameter access_token=<token> (browser EventSource)
//
// Parameters:
//   - log: Logger for failed attempts. Nil discards them.
//   - token: Configured events token. Empty rejects every request.
//
// Returns:
//   - fiber.Handler: Authentication middleware.
func NewEventsMiddleware(log *zerolog.Logger, token string) fiber.Handler {
	return newMiddleware(log, token, "events API token not configured", extractors.FromQuery(accessTokenKey))
}

// newMiddleware returns a Fiber middleware that compares the request's token
// to the configured one in constant time, by their SHA-256 hashes.
//
// Auth failure logs use notify=no so they never fan out through notification hooks.
//
// Parameters:
//   - log: Logger for failed attempts. Nil discards them.
//   - token: Configured token. Empty rejects every request.
//   - notConfigured: Response body when token is empty.
//   - fallback: Token source read after the Authorization header.
//
// Returns:
//   - fiber.Handler: Authentication middleware.
func newMiddleware(
	log *zerolog.Logger,
	token, notConfigured string,
	fallback extractors.Extractor,
) fiber.Handler {
	expectedHash := sha256.Sum256([]byte(token))

	authLog := zerolog.Nop()
	if log != nil {
		// Child logger for high-volume auth warnings. The composition root may
		// already pass a notify=no logger, but setting it here keeps auth safe
		// either way.
		authLog = log.With().Str("notify", "no").Logger()
	}

	return func(c fiber.Ctx) error {
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).SendString(notConfigured)
		}

		if !tokenMatches(&authLog, c, expectedHash, fallback) {
			return c.Status(fiber.StatusUnauthorized).SendString(keyauth.ErrMissingOrMalformedAPIKey.Error())
		}

		return c.Next()
	}
}

// tokenMatches reports whether the request carries a valid token.
func tokenMatches(
	log *zerolog.Logger,
	c fiber.Ctx,
	expectedHash [sha256.Size]byte,
	fallback extractors.Extractor,
) bool {
	provided, ok := extractToken(c, fallback)
	if !ok {
		log.Warn().Str("ip", c.IP()).Msg("Missing or malformed API key")

		return false
	}

	if !tokenHashMatches(expectedHash, provided) {
		log.Warn().Str("ip", c.IP()).Msg("Invalid token attempt")

		return false
	}

	return true
}

// extractToken returns the token from the request.
//
// Order:
//  1. Authorization Bearer scheme (RFC-style)
//  2. Raw Authorization header value (optional "Bearer " prefix stripped), for
//     Swagger UI apiKey security which places the typed value into Authorization
//  3. The fallback source
func extractToken(c fiber.Ctx, fallback extractors.Extractor) (string, bool) {
	token, err := extractors.FromAuthHeader("Bearer").Extract(c)
	if err == nil && token != "" {
		return token, true
	}

	raw := strings.TrimSpace(c.Get(fiber.HeaderAuthorization))
	if raw != "" {
		const bearerPrefix = "bearer "

		if len(raw) > len(bearerPrefix) && strings.EqualFold(raw[:len(bearerPrefix)], bearerPrefix) {
			raw = strings.TrimSpace(raw[len(bearerPrefix):])
		}

		if raw != "" {
			return raw, true
		}
	}

	token, err = fallback.Extract(c)
	if err == nil && token != "" {
		return token, true
	}

	return "", false
}

// tokenHashMatches compares a provided secret to the precomputed SHA-256 of the
// configured token using constant-time comparison.
func tokenHashMatches(expectedHash [sha256.Size]byte, provided string) bool {
	providedHash := sha256.Sum256([]byte(provided))

	return subtle.ConstantTimeCompare(expectedHash[:], providedHash[:]) == 1
}
