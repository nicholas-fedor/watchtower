// Package update provides an HTTP API handler for triggering Watchtower container updates.
// It manages update requests with concurrency control and image targeting.
//
// Key components:
//   - Handler: Processes HTTP requests to trigger updates.
//   - New: Creates a handler with an update function and lock.
//
// Usage example:
//
//	handler := update.New(updateFn, nil)
//	http.HandleFunc(handler.Path, handler.Handle)
//	logrus.Fatal(http.ListenAndServe(":8080", nil))
//
// The package uses a channel-based lock for concurrency and logrus for logging requests.
package update
