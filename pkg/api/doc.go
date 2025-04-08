// Package api provides an HTTP server for Watchtower’s API endpoints.
// It handles token-authenticated requests for triggering container updates.
//
// Key components:
//   - API: Manages server setup and endpoint registration.
//   - Handler: Wraps HTTP handlers with token validation.
//
// Usage example:
//
//	api := api.New("secure-token")
//	api.RegisterFunc("/v1/update", updateHandler)
//	if err := api.Start(ctx, true); err != nil {
//	    logrus.WithError(err).Error("API start failed")
//	}
//
// The package uses a custom ServeMux for routing, supports graceful shutdown,
// and integrates with logrus for logging server operations.
package api
