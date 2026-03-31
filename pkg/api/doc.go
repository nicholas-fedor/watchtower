// Package api provides an HTTP server for Watchtower's API endpoints.
// It handles token-authenticated requests for triggering container updates.
//
// Key components:
//   - API: Manages server setup, endpoint registration, and request authentication.
//   - RequireToken: Wraps HTTP handlers with token validation using SHA-256 hashing
//     and constant-time comparison to prevent timing side-channel attacks.
//   - RunHTTPServer: Starts and manages the HTTP server lifecycle with graceful shutdown.
//
// Security features:
//   - Token hashing: Tokens are hashed with SHA-256 at initialization and never stored in plaintext.
//   - Constant-time comparison: Uses crypto/subtle with data-independent timing to prevent timing attacks.
//   - Per-IP rate limiting: Limits authentication attempts to 60 requests per minute with burst of 10.
//
// Usage example:
//
//	// Load the API token from the WATCHTOWER_HTTP_API_TOKEN environment variable.
//	// Set it before running: export WATCHTOWER_HTTP_API_TOKEN="my-secret-token"
//	token := os.Getenv("WATCHTOWER_HTTP_API_TOKEN")
//
//	api := api.New(token, ":8080", 60)
//	api.RegisterFunc("POST /v1/update", updateHandler)
//
//	// Start(ctx, block, noStartupMessage)
//	//   block:            if true, Start blocks until the server shuts down;
//	//                     if false, the server runs in a background goroutine.
//	//   noStartupMessage: if true, suppresses the "Starting HTTP API server" log line.
//	err := api.Start(ctx, true, false)
//	if err != nil {
//	    logrus.WithError(err).Error("API start failed")
//	}
//
// The package uses a custom ServeMux for routing, supports graceful shutdown,
// and integrates with logrus for logging server operations.
package api
