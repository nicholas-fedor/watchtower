// Package api provides Watchtower's HTTP API server, built on Fiber v3.
//
// It handles token-authenticated requests for triggering container updates,
// serving Prometheus metrics, and listing watched container image identities.
// It also exposes standard Kubernetes health probe endpoints.
//
// Endpoints:
//
//	GET  /livez              Liveness probe — always returns 200 OK
//	GET  /readyz             Readiness probe — checks Docker client connectivity
//	GET  /startupz           Startup probe — always returns 200 OK
//	POST /v1/update          Trigger container update scan (requires auth)
//	GET  /v1/metrics         Prometheus metrics (requires auth)
//	GET  /v1/containers      List watched container image identities (requires auth)
//
// Health probes (/livez, /readyz, /startupz) are registered unconditionally
// and require no authentication. All /v1/* endpoints require Bearer token
// authentication.
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
