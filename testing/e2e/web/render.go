package web

import (
	"bytes"

	"github.com/a-h/templ"
	"github.com/gofiber/fiber/v3"
)

// render writes a templ component as UTF-8 HTML.
//
// Parameters:
//   - c: Fiber request context.
//   - component: Compiled templ tree.
//
// Returns:
//   - error: Render or write failure.
func render(c fiber.Ctx, component templ.Component) error {
	var buf bytes.Buffer

	err := component.Render(c.Context(), &buf)
	if err != nil {
		return err
	}

	c.Set("Content-Type", "text/html; charset=utf-8")

	return c.Send(buf.Bytes())
}
