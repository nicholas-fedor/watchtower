package api

import (
	"context"
	"net/http"
	"net/url"

	"github.com/nicholas-fedor/watchtower/testing/e2e/store"
	"github.com/nicholas-fedor/watchtower/testing/e2e/stream"
)

// ListCases returns cases for a sitting.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting UUID.
//   - status: Optional status filter.
//   - query: Optional substring on case id or factors.
//
// Returns:
//   - []store.Case: Matching cases.
//   - int: Total matches.
//   - error: Transport or API failure.
func (c *Client) ListCases(ctx context.Context, runID, status, query string) ([]store.Case, int, error) {
	path := "/v1/runs/" + url.PathEscape(runID) + "/cases"
	q := url.Values{}
	if status != "" {
		q.Set("status", status)
	}

	if query != "" {
		q.Set("q", query)
	}

	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	wrap, decErr := decodeJSON[struct {
		Cases []store.Case `json:"cases"`
		Total int          `json:"total"`
	}](resp)
	if decErr != nil {
		return nil, 0, decErr
	}

	return wrap.Cases, wrap.Total, nil
}

// Logs returns Watchtower streams for one case.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting UUID.
//   - caseID: Case identifier.
//   - streamName: stdout, stderr, or empty for both.
//
// Returns:
//   - []stream.Line: Log lines.
//   - error: Transport or API failure.
func (c *Client) Logs(ctx context.Context, runID, caseID, streamName string) ([]stream.Line, error) {
	path := "/v1/runs/" + url.PathEscape(runID) + "/cases/" + url.PathEscape(caseID) + "/logs"
	if streamName != "" {
		path += "?" + url.Values{"stream": {streamName}}.Encode()
	}

	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	wrap, decErr := decodeJSON[struct {
		Lines []stream.Line `json:"lines"`
	}](resp)
	if decErr != nil {
		return nil, decErr
	}

	return wrap.Lines, nil
}
