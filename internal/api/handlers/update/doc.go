// Package update provides an HTTP API handler for triggering Watchtower container
// updates. It manages update requests with concurrency control, image targeting,
// and asynchronous execution.
//
// The handler supports:
//   - Lock-based concurrency control (targeted updates block, full updates fail fast)
//   - Targeted updates via ?image= query parameters (comma-separated, repeatable)
//   - Full updates (scan all containers)
//   - Async execution via ?async=true (returns 202 Accepted immediately)
//   - Sync execution (returns 200 with JSON results)
//
// Shutdown is handled cooperatively — the handler listens on c.Context().Done()
// and returns 503 when the request context is cancelled while waiting for the lock.
package update
