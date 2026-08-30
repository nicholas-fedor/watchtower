package api

import (
	"context"
	"fmt"
	"time"
)

// Wait polls a sitting until it leaves queued or running.
//
// Parameters:
//   - ctx: Cancellation.
//   - id: Run UUID.
//
// Returns:
//   - Run: Terminal snapshot.
//   - error: Transport failure or ctx done.
func (c *Client) Wait(ctx context.Context, id string) (Run, error) {
	ticker := time.NewTicker(waitTick)
	defer ticker.Stop()

	for {
		run, err := c.GetRun(ctx, id)
		if err != nil {
			return Run{}, err
		}

		switch run.Status {
		case RunQueued, RunRunning:
		default:
			return run, nil
		}

		select {
		case <-ctx.Done():
			return Run{}, fmt.Errorf("wait run %s: %w", id, ctx.Err())
		case <-ticker.C:
		}
	}
}
