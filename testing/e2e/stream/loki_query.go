package stream

import (
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Query reads a case stream from Loki.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting id.
//   - caseID: Case id.
//   - streamName: stdout, stderr, or empty for both.
//
// Returns:
//   - []Line: Matching lines.
//   - error: HTTP or decode failure.
func (l *Loki) Query(ctx context.Context, runID, caseID, streamName string) ([]Line, error) {
	selector := fmt.Sprintf(`{job="watchtower-e2e",run_id="%s",case_id="%s"}`, escapeLabel(runID), escapeLabel(caseID))
	if streamName != "" {
		selector = fmt.Sprintf(`{job="watchtower-e2e",run_id="%s",case_id="%s",stream="%s"}`, escapeLabel(runID), escapeLabel(caseID), escapeLabel(streamName))
	}

	params := url.Values{}
	params.Set("query", selector)
	params.Set("limit", lokiQueryLimit)
	params.Set("direction", "forward")
	params.Set("start", strconv.FormatInt(time.Now().Add(-lokiLookback).UnixNano(), 10))
	params.Set("end", strconv.FormatInt(time.Now().UnixNano(), 10))

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, l.base+"/loki/api/v1/query_range?"+params.Encode(), nil)
	if reqErr != nil {
		return nil, fmt.Errorf("loki query request: %w", reqErr)
	}

	resp, doErr := l.client.Do(req)
	if doErr != nil {
		return nil, fmt.Errorf("loki query: %w", doErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		slurp, readErr := io.ReadAll(io.LimitReader(resp.Body, lokiErrorBodyLimit))
		if readErr != nil {
			return nil, fmt.Errorf("%w: query status %d: %v", ErrHTTP, resp.StatusCode, readErr)
		}

		return nil, fmt.Errorf("%w: query status %d: %s", ErrHTTP, resp.StatusCode, slurp)
	}

	var parsed lokiQuery

	decErr := json.UnmarshalRead(resp.Body, &parsed)
	if decErr != nil {
		return nil, fmt.Errorf("loki query decode: %w", decErr)
	}

	out := make([]Line, 0)

	for _, result := range parsed.Data.Result {
		name := result.Stream["stream"]
		for _, pair := range result.Values {
			if len(pair) < 2 {
				continue
			}

			ns, _ := strconv.ParseInt(pair[0], 10, 64)
			out = append(out, Line{
				Time:   time.Unix(0, ns).UTC(),
				Body:   pair[1],
				Stream: name,
			})
		}
	}

	return out, nil
}
