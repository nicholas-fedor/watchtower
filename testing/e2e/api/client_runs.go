package api

import (
	"context"
	"net/http"
	"net/url"
)

// CreateRun POSTs a sitting spec.
//
// Parameters:
//   - ctx: Cancellation.
//   - spec: Operator input.
//
// Returns:
//   - Run: Queued sitting.
//   - error: Transport or API failure.
func (c *Client) CreateRun(ctx context.Context, spec Spec) (Run, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/runs", spec)
	if err != nil {
		return Run{}, err
	}
	defer resp.Body.Close()

	return decodeJSON[Run](resp)
}

// GetRun loads one sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - id: Run UUID.
//
// Returns:
//   - Run: Snapshot.
//   - error: Transport or API failure.
func (c *Client) GetRun(ctx context.Context, id string) (Run, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(id), nil)
	if err != nil {
		return Run{}, err
	}
	defer resp.Body.Close()

	return decodeJSON[Run](resp)
}

// ListRuns returns sittings newest first.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - []Run: Page of sittings.
//   - error: Transport or API failure.
func (c *Client) ListRuns(ctx context.Context) ([]Run, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/runs", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	wrap, decErr := decodeJSON[struct {
		Runs []Run `json:"runs"`
	}](resp)
	if decErr != nil {
		return nil, decErr
	}

	return wrap.Runs, nil
}

// Resume re-queues an interrupted sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - id: Run UUID.
//
// Returns:
//   - Run: Re-queued snapshot.
//   - error: Transport or API failure.
func (c *Client) Resume(ctx context.Context, id string) (Run, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(id)+"/resume", nil)
	if err != nil {
		return Run{}, err
	}
	defer resp.Body.Close()

	return decodeJSON[Run](resp)
}

// Cancel stops a queued or running sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - id: Run UUID.
//
// Returns:
//   - Run: Canceled snapshot.
//   - error: Transport or API failure.
func (c *Client) Cancel(ctx context.Context, id string) (Run, error) {
	resp, err := c.do(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(id)+"/cancel", nil)
	if err != nil {
		return Run{}, err
	}
	defer resp.Body.Close()

	return decodeJSON[Run](resp)
}
