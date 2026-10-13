package ui

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

const (
	// BasePath is the path the application shell is served from.
	BasePath = "/ui/"
	// AssetsPath is the route pattern for embedded static assets.
	AssetsPath = "/ui/assets/*"
	// SessionPath is the path of the sign-in state resource.
	SessionPath = "/ui/session"

	// CookieName is the session cookie read by the API auth middleware.
	CookieName = "access_token"

	// sessionLifetime is how long a browser stays signed in.
	sessionLifetime = 30 * 24 * time.Hour

	// staticRoot is the embedded directory holding the application files.
	staticRoot = "static"
	// indexFile is the application shell within staticRoot.
	indexFile = "index.html"

	// contentSecurityPolicy restricts the page to its own embedded resources.
	contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; " +
		"img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

	// cacheRevalidate makes browsers revalidate with the ETag on every load.
	cacheRevalidate = "no-cache"
	// cacheFonts lets browsers reuse font files without asking again.
	cacheFonts = "public, max-age=86400"
)

//go:embed static
var staticFiles embed.FS

// Features reports which HTTP API endpoints the UI can use.
type Features struct {
	// Containers is true when the containers endpoints are enabled.
	Containers bool `json:"containers"`
	// Check is true when the check endpoint is enabled.
	Check bool `json:"check"`
	// Update is true when the update endpoint is enabled.
	Update bool `json:"update"`
	// History is true when the history endpoint is enabled.
	History bool `json:"history"`
	// Config is true when the config endpoint is enabled.
	Config bool `json:"config"`
	// Images is true when the images endpoint is enabled.
	Images bool `json:"images"`
	// Metrics is true when the metrics and status endpoints are enabled.
	Metrics bool `json:"metrics"`
	// Swagger is true when Swagger UI is enabled.
	Swagger bool `json:"swagger"`
}

// Options configures the UI handler.
type Options struct {
	// Token is the HTTP API token that sign-in is checked against.
	Token string
	// Version is the Watchtower version shown in the UI.
	Version string
	// Features lists the endpoints available to the UI.
	Features Features
}

// asset is one embedded file prepared for serving.
type asset struct {
	body         []byte
	contentType  string
	etag         string
	cacheControl string
}

// Handler serves the web UI shell, its assets, and the sign-in resource.
type Handler struct {
	log *zerolog.Logger

	version      string
	features     Features
	tokenSet     bool
	expectedHash [sha256.Size]byte
	assets       map[string]asset
}

// signInRequest is the JSON body of a sign-in request.
type signInRequest struct {
	Token string `json:"token"`
}

// New creates a UI handler and loads the embedded application files.
//
// Parameters:
//   - log: Logger for sign-in events. Nil disables logging.
//   - opts: Token, version, and enabled features.
//
// Returns:
//   - *Handler: Handler ready to register.
//   - error: Non-nil if the embedded files cannot be read.
func New(log *zerolog.Logger, opts Options) (*Handler, error) {
	if log == nil {
		nop := zerolog.Nop()
		log = &nop
	}

	// Sign-in attempts are high-volume and must not fan out to notifications.
	uiLog := log.With().Str("notify", "no").Logger()

	assets, err := loadAssets()
	if err != nil {
		return nil, err
	}

	return &Handler{
		log:          &uiLog,
		version:      opts.Version,
		features:     opts.Features,
		tokenSet:     opts.Token != "",
		expectedHash: sha256.Sum256([]byte(opts.Token)),
		assets:       assets,
	}, nil
}

// loadAssets reads every embedded file into memory, keyed by its path
// relative to the static root.
func loadAssets() (map[string]asset, error) {
	assets := make(map[string]asset)

	err := fs.WalkDir(staticFiles, staticRoot, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return nil
		}

		body, readErr := staticFiles.ReadFile(name)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", name, readErr)
		}

		sum := sha256.Sum256(body)
		contentType, cacheControl := describeAsset(name)

		assets[strings.TrimPrefix(name, staticRoot+"/")] = asset{
			body:         body,
			contentType:  contentType,
			etag:         `"` + hex.EncodeToString(sum[:8]) + `"`,
			cacheControl: cacheControl,
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load embedded UI files: %w", err)
	}

	return assets, nil
}

// describeAsset returns the content type and cache policy for a file name.
func describeAsset(name string) (string, string) {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8", cacheRevalidate
	case ".css":
		return "text/css; charset=utf-8", cacheRevalidate
	case ".js":
		return "text/javascript; charset=utf-8", cacheRevalidate
	case ".svg":
		return "image/svg+xml", cacheRevalidate
	case ".woff2":
		return "font/woff2", cacheFonts
	case ".txt":
		return "text/plain; charset=utf-8", cacheRevalidate
	default:
		return fiber.MIMEOctetStream, cacheRevalidate
	}
}

// Redirect sends the browser to the application shell.
func (h *Handler) Redirect(c fiber.Ctx) error {
	c.Set(fiber.HeaderLocation, BasePath)

	return sendStatus(c, fiber.StatusFound)
}

// Index serves the application shell.
func (h *Handler) Index(c fiber.Ctx) error {
	return h.send(c, indexFile)
}

// Asset serves one embedded static file.
func (h *Handler) Asset(c fiber.Ctx) error {
	return h.send(c, c.Params("*"))
}

// Session reports whether the browser is signed in and which features are enabled.
func (h *Handler) Session(c fiber.Ctx) error {
	return h.sendSession(c, h.matches(c.Cookies(CookieName)))
}

// SignIn checks the submitted token and, when it matches, sets the session cookie.
func (h *Handler) SignIn(c fiber.Ctx) error {
	var request signInRequest

	// A body that is not JSON carries no token, so it fails the same way a wrong token does.
	_ = json.Unmarshal(c.Body(), &request)

	if !h.matches(request.Token) {
		h.log.Warn().Str("ip", c.IP()).Msg("Rejected web UI sign-in")

		return h.sendError(c, fiber.StatusUnauthorized, "token does not match")
	}

	c.Cookie(&fiber.Cookie{
		Name:     CookieName,
		Value:    request.Token,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		Secure:   c.Protocol() == "https",
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteStrictMode,
	})

	return h.sendSession(c, true)
}

// SignOut clears the session cookie.
func (h *Handler) SignOut(c fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		Secure:   c.Protocol() == "https",
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteStrictMode,
	})

	return h.sendSession(c, false)
}

// send writes an embedded file, answering 304 when the browser's copy is current.
func (h *Handler) send(c fiber.Ctx, name string) error {
	file, ok := h.assets[name]
	if !ok {
		return sendStatus(c, fiber.StatusNotFound)
	}

	c.Set(fiber.HeaderContentSecurityPolicy, contentSecurityPolicy)
	c.Set(fiber.HeaderETag, file.etag)
	c.Set(fiber.HeaderCacheControl, file.cacheControl)

	if c.Get(fiber.HeaderIfNoneMatch) == file.etag {
		return sendStatus(c, fiber.StatusNotModified)
	}

	c.Set(fiber.HeaderContentType, file.contentType)

	err := c.Send(file.body)
	if err != nil {
		return fmt.Errorf("failed to send %s: %w", name, err)
	}

	return nil
}

// sendStatus writes a bodiless response with the given status.
func sendStatus(c fiber.Ctx, status int) error {
	err := c.SendStatus(status)
	if err != nil {
		return fmt.Errorf("failed to send status %d: %w", status, err)
	}

	return nil
}

// matches reports whether the provided token equals the configured one,
// using a constant-time comparison of their SHA-256 hashes.
func (h *Handler) matches(provided string) bool {
	if !h.tokenSet || provided == "" {
		return false
	}

	providedHash := sha256.Sum256([]byte(provided))

	return subtle.ConstantTimeCompare(h.expectedHash[:], providedHash[:]) == 1
}

// sendSession writes the sign-in state. Version and features are only
// disclosed to signed-in browsers.
func (h *Handler) sendSession(c fiber.Ctx, authenticated bool) error {
	c.Set(fiber.HeaderCacheControl, "no-store")

	body := fiber.Map{"authenticated": authenticated}
	if authenticated {
		body["version"] = h.version
		body["features"] = h.features
	}

	err := c.Status(fiber.StatusOK).JSON(body)
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}

// sendError writes a JSON error body with the given status.
func (h *Handler) sendError(c fiber.Ctx, status int, message string) error {
	c.Set(fiber.HeaderCacheControl, "no-store")

	err := c.Status(status).JSON(fiber.Map{"error": message})
	if err != nil {
		return fmt.Errorf("failed to send JSON response: %w", err)
	}

	return nil
}
