package server

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
)

const (
	// shutdownHTTP is how long we wait for in-flight HTTP (including SSE) to end.
	shutdownHTTP = 8 * time.Second
)

// shutdownApp closes listeners and waits for connections, then force-closes.
//
// SSE to /v1/runs/{id}/events never goes idle, so Shutdown without a deadline
// hangs. ShutdownWithContext force-closes after timeout.
//
// Parameters:
//   - app: Fiber application that is serving.
//   - timeout: Maximum wait. Values below 1ms use shutdownHTTP.
//
// Returns:
//   - error: Shutdown failure other than deadline.
func shutdownApp(app *fiber.App, timeout time.Duration) error {
	if timeout < time.Millisecond {
		timeout = shutdownHTTP
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := app.ShutdownWithContext(shutCtx)
	if err == nil || shutCtx.Err() != nil {
		return nil
	}

	return err
}
