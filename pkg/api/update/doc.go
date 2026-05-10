// Package update provides an HTTP API handler for triggering Watchtower container updates.
// It manages update requests with concurrency control, image targeting, and asynchronous execution.
//
// Key components:
//   - Handler: Processes HTTP requests to trigger updates with lock-based synchronization.
//   - New: Creates a handler with an update function and optional lock channel.
//
// Security features:
//   - Request body size limiting: Caps request bodies at 1 MiB to prevent resource exhaustion.
//
// Usage example:
//
//	handler := update.New(updateFn, nil)
//	http.HandleFunc("POST "+handler.Path, handler.Handle)
//	logrus.Fatal(http.ListenAndServe(":8080", nil))
//
// The package uses a channel-based lock for concurrency, supports both targeted and
// full updates with different lock acquisition strategies, and integrates with logrus
// for logging requests. Asynchronous execution is supported via the "async" query parameter.
package update
