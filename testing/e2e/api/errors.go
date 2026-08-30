package api

import (
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gofiber/fiber/v3"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
)

// fiberError maps handler errors to JSON status responses.
//
// Parameters:
//   - c: Fiber request context.
//   - err: Handler error.
//
// Returns:
//   - error: Write failure, or nil after a JSON error body is sent.
func fiberError(c fiber.Ctx, err error) error {
	if herr, ok := errors.AsType[*huma.ErrorModel](err); ok {
		return c.Status(herr.Status).JSON(herr)
	}

	if fe, ok := errors.AsType[*fiber.Error](err); ok {
		return c.Status(fe.Code).JSON(fiber.Map{"error": fe.Message})
	}

	return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
}

// mapStoreErr converts store sentinel errors to Huma 404/409 responses.
//
// Parameters:
//   - err: Store error.
//
// Returns:
//   - error: Huma status error, or err unchanged.
func mapStoreErr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return huma.Error404NotFound(err.Error())
	}

	if errors.Is(err, store.ErrConflict) {
		return huma.Error409Conflict(err.Error())
	}

	return err
}
