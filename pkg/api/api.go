package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
)

const (
	// serverReadTimeout defines the maximum duration for reading the request, including headers.
	serverReadTimeout = 10 * time.Second
	// serverWriteTimeout defines the maximum duration for writing the response.
	serverWriteTimeout = 10 * time.Minute
	// serverIdleTimeout defines the maximum duration for keeping idle connections alive.
	serverIdleTimeout = 30 * time.Second
	// serverMaxHeaderShift defines the bit shift for the maximum header size (1 MB).
	serverMaxHeaderShift = 20
	// serverShutdownTimeout defines the maximum duration allowed for the server to shut down gracefully.
	serverShutdownTimeout = 5 * time.Second

	// rateLimitBurst defines the burst capacity for the rate limiter.
	// Allows short bursts of legitimate requests (e.g., concurrent dashboard updates)
	// while still protecting against sustained brute-force attacks.
	rateLimitBurst = 10
	// defaultRateLimitPerMinute is the fallback rate limit when a non-positive value is provided.
	defaultRateLimitPerMinute = 60
	// secondsPerMinute is used to convert per-minute rate to per-second for the rate limiter.
	secondsPerMinute = 60

	// limiterCleanupInterval defines how often stale per-IP limiters are scanned and removed.
	limiterCleanupInterval = 5 * time.Minute
	// limiterTTL defines how long a per-IP limiter is retained without activity before removal.
	limiterTTL = 10 * time.Minute
)

// Errors for API server operations.
var (
	// errServerFailed indicates a failure to start or run the HTTP server.
	errServerFailed = errors.New("http server failed")
	// errServerShutdownFailed indicates a failure during the HTTP server shutdown process.
	errServerShutdownFailed = errors.New("server shutdown failed")
)

// HTTPServer defines the interface for an HTTP server.
type HTTPServer interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

// ipLimiter wraps a rate limiter with a lastSeen timestamp for cleanup tracking.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// API is the HTTP server responsible for serving the HTTP API endpoints.
//
// It provides token-based authentication with SHA-256 hashing and constant-time
// comparison to mitigate timing attacks, per-IP rate limiting to prevent brute-force
// attempts, and configurable server timeouts for resource protection.
type API struct {
	tokenHash          [sha256.Size]byte // SHA-256 hash of the authentication token.
	emptyToken         bool              // True if the token was empty at initialization.
	Addr               string            // Set dynamically from flags.
	hasHandlers        bool
	mux                *http.ServeMux        // Custom mux to avoid global collisions.
	server             HTTPServer            // Optional injected server for testing.
	rateLimitPerMinute int                   // Maximum authentication requests per minute per IP.
	limiterIPs         map[string]*ipLimiter // Per-IP rate limiter for authentication.
	limiterMu          sync.Mutex            // Mutex protecting the limiterIPs map.
}

// New is a factory function creating a new API instance.
//
// The token is hashed using SHA-256 at initialization and stored securely for
// constant-time comparison during authentication, preventing timing side-channel attacks.
// The server parameter is optional and allows dependency injection for testing.
//
// Parameters:
//   - token: The authentication token for API access. Must be non-empty for the server to start.
//   - addr: The address (host:port) to bind the HTTP server to.
//   - rateLimitPerMinute: Maximum authentication requests per minute per IP address.
//   - server: Optional HTTPServer implementation for testing; if not provided, a real http.Server is used.
//
// Returns:
//   - *API: Initialized API instance ready for handler registration and server startup.
func New(token, addr string, rateLimitPerMinute int, server ...HTTPServer) *API {
	var injectedServer HTTPServer
	if len(server) > 0 {
		injectedServer = server[0]
	}

	// Clamp non-positive rate limits to a sensible default to prevent
	// a zero or negative rate from breaking the token-bucket limiter.
	if rateLimitPerMinute <= 0 {
		rateLimitPerMinute = defaultRateLimitPerMinute
	}

	api := &API{
		tokenHash:          sha256.Sum256([]byte(token)),
		emptyToken:         token == "",
		Addr:               addr,
		hasHandlers:        false,
		mux:                http.NewServeMux(),
		server:             injectedServer,
		rateLimitPerMinute: rateLimitPerMinute,
		limiterIPs:         make(map[string]*ipLimiter),
	}
	logrus.WithFields(
		logrus.Fields{
			"addr":      api.Addr,
			"token_len": len(token),
		},
	).Debug("Initialized new API instance")

	return api
}

// RequireToken is a wrapper around http.HandlerFunc that checks token validity
// and enforces per-IP rate limiting on authentication attempts.
//
// Token comparison uses constant-time comparison via crypto/subtle.ConstantTimeCompare
// wrapped in WithDataIndependentTiming to mitigate timing side-channel attacks.
func (api *API) RequireToken(handleFunc http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP for rate limiting.
		clientIP := extractIP(r.RemoteAddr)

		// Check per-IP rate limiter before authentication.
		if !api.allowRequest(clientIP) {
			logrus.WithField("ip", clientIP).
				Warn("Rate limit exceeded for authentication attempts")
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		// Extract and hash the provided token for constant-time comparison.
		// Strip the "Bearer " prefix to extract the raw token before hashing.
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		rawToken := strings.TrimPrefix(auth, "Bearer ")
		providedHash := sha256.Sum256([]byte(rawToken))

		// Use constant-time comparison with data-independent timing to prevent
		// timing side-channel attacks on token validation.
		var match int

		subtle.WithDataIndependentTiming(
			func() {
				match = subtle.ConstantTimeCompare(
					providedHash[:],
					api.tokenHash[:],
				)
			})

		if match != 1 {
			logrus.WithField("token_len", len(auth)).
				Warn("Invalid token attempt detected")
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		logrus.WithField("path", r.URL.Path).
			Debug("Valid token authenticated")
		handleFunc(w, r)
	}
}

// RegisterFunc is a wrapper around http.HandleFunc that also sets the flag used to determine whether to launch the API.
func (api *API) RegisterFunc(path string, fn http.HandlerFunc) {
	api.hasHandlers = true
	api.mux.HandleFunc(path, api.RequireToken(fn))
	logrus.WithField("path", path).
		Debug("Registered API function handler")
}

// RegisterHandler is a wrapper around http.Handler that also sets the flag used to determine whether to launch the API.
func (api *API) RegisterHandler(path string, handler http.Handler) {
	api.hasHandlers = true
	api.mux.Handle(
		path,
		api.RequireToken(handler.ServeHTTP),
	)
	logrus.WithField("path", path).
		Debug("Registered API handler")
}

// Start launches the API server over HTTP, requiring a non-empty token.
func (api *API) Start(ctx context.Context, block, noStartupMessage bool) error {
	if !api.hasHandlers {
		logrus.WithField("addr", api.Addr).
			Debug("No handlers registered, skipping API start")

		return nil
	}

	if api.emptyToken {
		logrus.WithField("addr", api.Addr).
			Fatal("API token is empty or unset")
	}

	var server HTTPServer
	if api.server != nil {
		// Use injected server for testing
		server = api.server
	} else {
		// Create real server for production
		server = &http.Server{
			Addr:              api.Addr,
			Handler:           api.mux,
			ReadTimeout:       serverReadTimeout,
			WriteTimeout:      serverWriteTimeout,
			IdleTimeout:       serverIdleTimeout,
			ReadHeaderTimeout: serverReadTimeout,
			MaxHeaderBytes:    1 << serverMaxHeaderShift,
			TLSConfig:         nil,
			TLSNextProto:      make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
			BaseContext:       func(_ net.Listener) context.Context { return ctx },
		}
	}

	// Start background goroutine to clean up stale per-IP rate limiters.
	go api.cleanupStaleLimiters(ctx)

	if !noStartupMessage {
		logrus.WithField("addr", api.Addr).
			Info("Starting HTTP API server")
	}

	if block {
		return RunHTTPServer(ctx, server)
	}

	go func() {
		err := RunHTTPServer(ctx, server)
		if err != nil {
			logrus.WithError(err).
				WithField("addr", api.Addr).
				Debug("HTTP server encountered an error")
		}
	}()

	return nil
}

// cleanupStaleLimiters periodically removes per-IP rate limiters that have not
// been accessed within the limiterTTL duration. It stops when the provided
// context is canceled.
//
// Parameters:
//   - ctx: Context for cancellation; the cleanup loop exits when ctx is done.
func (api *API) cleanupStaleLimiters(ctx context.Context) {
	ticker := time.NewTicker(limiterCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			api.limiterMu.Lock()

			now := time.Now()
			for ip, entry := range api.limiterIPs {
				if now.Sub(entry.lastSeen) > limiterTTL {
					delete(api.limiterIPs, ip)
				}
			}

			api.limiterMu.Unlock()
		}
	}
}

// allowRequest checks whether the given IP is within the rate limit.
//
// It creates a per-IP rate limiter on first access with a configurable
// request-per-minute rate and burst capacity. Returns true if the request
// is allowed, false if rate-limited.
//
// Parameters:
//   - clientIP: The client's IP address used to track per-IP rate limits.
//
// Returns:
//   - bool: true if the request is allowed, false if the client has exceeded the rate limit.
func (api *API) allowRequest(clientIP string) bool {
	api.limiterMu.Lock()

	entry, exists := api.limiterIPs[clientIP]
	if !exists {
		entry = &ipLimiter{
			limiter: rate.NewLimiter(
				rate.Limit(api.rateLimitPerMinute)/secondsPerMinute,
				rateLimitBurst,
			),
		}
		api.limiterIPs[clientIP] = entry
	}

	entry.lastSeen = time.Now()

	api.limiterMu.Unlock()

	return entry.limiter.Allow()
}

// extractIP parses the host IP from an "IP:port" string (e.g., r.RemoteAddr).
//
// If parsing fails, it returns the original value or "unknown" for empty input.
//
// Parameters:
//   - addr: The address string, typically from r.RemoteAddr in format "IP:port".
//
// Returns:
//   - string: The host IP portion only, without the port.
func extractIP(addr string) string {
	if addr == "" {
		return "unknown"
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// If SplitHostPort fails (e.g., no port present), use the original value.
		return addr
	}

	return host
}

// RunHTTPServer starts the HTTP server with configured timeouts and handlers.
//
// It launches the server in a goroutine and blocks until either the server
// encounters an error or the provided context is canceled. On context cancellation,
// it initiates a graceful shutdown with a configurable timeout.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control. Cancellation triggers graceful shutdown.
//   - server: The HTTPServer instance to start and manage.
//
// Returns:
//   - error: Non-nil if the server fails to start or shutdown encounters an error,
//     nil on clean shutdown.
func RunHTTPServer(ctx context.Context, server HTTPServer) error {
	errChan := make(chan error, 1)

	go func() {
		logrus.Debug("Launching HTTP server listener")

		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- fmt.Errorf("%w: %w", errServerFailed, err)
		} else {
			errChan <- nil
		}
	}()

	select {
	case err := <-errChan:
		if err != nil {
			logrus.WithError(err).Error("HTTP server failed to start or run")
		}

		return err
	case <-ctx.Done():
		logrus.Info("Initiating HTTP server shutdown due to context cancellation")

		// Use context.Background() as the parent because ctx is already canceled
		// at this point; deriving from it would produce an immediately-expired
		// shutdown context, defeating the serverShutdownTimeout.
		shutdownCtx, shutdownCancel := context.WithTimeout(
			context.Background(),
			serverShutdownTimeout,
		)
		defer shutdownCancel()

		err := server.Shutdown(shutdownCtx) //nolint:contextcheck // shutdownCtx is intentionally derived from Background(), not the canceled ctx
		if err != nil && !errors.Is(err, context.Canceled) {
			logrus.WithError(err).
				Debug("Failed to shut down HTTP server")

			return fmt.Errorf("%w: %w", errServerShutdownFailed, err)
		}

		logrus.Info("HTTP server shut down successfully")

		return nil
	}
}
