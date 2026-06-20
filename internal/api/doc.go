// Package api provides Watchtower's HTTP API server, built on Fiber v3.
//
// It handles token-authenticated requests for triggering container updates,
// checking update availability, serving Prometheus metrics, listing watched
// container image identities, and streaming real-time events via SSE.
//
// Endpoints:
//
//	GET  /livez                  Liveness probe — always returns 200 OK
//	GET  /readyz                 Readiness probe — checks Docker client connectivity
//	GET  /startupz               Startup probe — always returns 200 OK
//	POST /v1/update              Trigger container update scan (requires auth)
//	POST /v1/check               Check for available updates (requires auth)
//	GET  /v1/metrics             Prometheus metrics (requires auth)
//	GET  /v1/status              Last scan results as JSON (requires auth)
//	GET  /v1/containers          List watched container statuses (requires auth)
//	GET  /v1/containers/details  Detailed container info with config flags (requires auth)
//	GET  /v1/history             Historical scan results from ring buffer (requires auth)
//	GET  /v1/images              Tracked images with digests and container counts (requires auth)
//	GET  /v1/config              Active configuration settings (requires auth)
//	GET  /v1/events              Real-time operational events via SSE (no auth)
//	GET  /swagger/*              Swagger UI (requires --http-api-swagger)
//
// Health probes (/livez, /readyz, /startupz) are registered unconditionally
// and require no authentication. All /v1/* endpoints except /v1/events require
// Bearer token authentication.
//
// Key components:
//   - New: Creates a Fiber application with the configured middleware stack.
//   - newKeyAuthMiddleware: Bearer token authentication using SHA-256 hashing
//     and constant-time comparison to prevent timing side-channel attacks.
//   - registerHealthChecks: Registers liveness, readiness, and startup probes.
//   - SetupAndStartAPI: Orchestrates endpoint registration and server lifecycle.
//
// Security features:
//   - Token hashing: Tokens are hashed with SHA-256 at initialization and never stored in plaintext.
//   - Constant-time comparison: Uses crypto/subtle to prevent timing attacks.
//   - Per-IP rate limiting: Sliding window rate limiting via Fiber's limiter middleware.
//   - Panic recovery: Catches handler panics and returns 500.
//   - Security headers: X-Content-Type-Options, X-Frame-Options, X-XSS-Protection.
//   - Request ID: Unique ID per request for log correlation.
//   - Response compression: gzip, deflate, brotli, zstd.
//
// Middleware stack (outermost to innermost):
//  1. recover     — panic recovery
//  2. helmet      — security headers
//  3. requestid   — request ID propagation
//  4. logger      — structured request logging via logrus
//  5. compress    — response compression
//  6. limiter     — per-IP rate limiting (sliding window)
//  7. keyauth     — Bearer token authentication (per-route)
package api
