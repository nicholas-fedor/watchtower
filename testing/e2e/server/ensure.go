package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/api"
)

const (
	// ensureWait is how long Ensure waits for /v1/health.
	ensureWait = 60 * time.Second
	// ensureTick is the health poll interval while starting Listen.
	ensureTick = 250 * time.Millisecond
)

var ensureMu sync.Mutex

// ensureLive is true while an in-process Listen goroutine should be treated as
// the owner of the control-plane port.
var ensureLive bool

// Ensure starts Listen in-process when no control plane is healthy yet.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: Listen failed, or health did not arrive in time.
func Ensure(ctx context.Context) error {
	errCh := make(chan error, 1)

	return ensure(ctx, api.NewClient(), func(ctx context.Context) {
		go func() {
			err := Listen(ctx, "", "")
			if err != nil {
				ensureMu.Lock()
				ensureLive = false
				ensureMu.Unlock()
			}
			errCh <- err
		}()
	}, errCh)
}

// ensure waits for cli to become healthy, starting at most one Listen.
//
// Parameters:
//   - ctx: Cancellation.
//   - cli: Health probe.
//   - start: Invoked once when this process should start Listen.
//   - errCh: Listen result. Nil skips the Listen-error path (tests).
//
// Returns:
//   - error: Listen failure, ctx done, or the deadline elapsed.
func ensure(ctx context.Context, cli *api.Client, start func(context.Context), errCh <-chan error) error {
	if cli.Healthy(ctx) {
		return nil
	}

	ensureMu.Lock()
	if !ensureLive {
		ensureLive = true
		start(ctx)
	}
	ensureMu.Unlock()

	deadline := time.Now().Add(ensureWait)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if cli.Healthy(ctx) {
			return nil
		}

		timer := time.NewTimer(ensureTick)
		select {
		case <-ctx.Done():
			timer.Stop()

			return ctx.Err()
		case err, ok := <-errCh:
			timer.Stop()
			if ok && err != nil {
				return err
			}
		case <-timer.C:
		}
	}

	return fmt.Errorf("%w at %s", ErrNotReady, cli.Base())
}
