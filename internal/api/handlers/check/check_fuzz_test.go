package check

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzExtractFilterParams verifies that extractFilterParams never panics and
// correctly parses query parameters with various delimiters and encodings.
func FuzzExtractFilterParams(f *testing.F) {
	f.Add("name", "nginx,redis,mysql")
	f.Add("name", "nginx")
	f.Add("name", "")
	f.Add("name", "nginx redis")
	f.Add("name", "nginx,,redis")
	f.Add("name", ",nginx,")
	f.Add("name", "nginx%20redis")
	f.Add("image", "nginx:latest,redis:7")
	f.Add("image", strings.Repeat("a,", 100))
	f.Add("image", "nginx:latest,redis:7,mysql:8.0,postgres:15")

	f.Fuzz(func(t *testing.T, key, rawValue string) {
		app := fiber.New(fiber.Config{})
		app.Get("/test", func(c fiber.Ctx) error {
			results := extractFilterParams(c, key)

			for _, r := range results {
				assert.NotEmpty(t, r, "extractFilterParams should not return empty strings")
			}

			return c.SendString("ok")
		})

		encodedValue := strings.ReplaceAll(rawValue, " ", "%20")
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test?"+key+"="+encodedValue, nil)
		resp, err := app.Test(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		assert.True(t, resp.StatusCode >= 200 && resp.StatusCode < 500,
			"expected valid status code, got %d", resp.StatusCode)
	})
}
