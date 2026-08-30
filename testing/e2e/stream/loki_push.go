package stream

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Push appends lines to Loki.
//
// Parameters:
//   - ctx: Cancellation.
//   - runID: Sitting id.
//   - caseID: Case id.
//   - streamName: stdout or stderr.
//   - lines: Lines to push.
//
// Returns:
//   - error: HTTP or marshal failure.
func (l *Loki) Push(ctx context.Context, runID, caseID, streamName string, lines []Line) error {
	if len(lines) == 0 {
		return nil
	}

	values := make([][2]string, 0, len(lines))
	for _, line := range lines {
		ts := line.Time
		if ts.IsZero() {
			ts = time.Now().UTC()
		}

		values = append(values, [2]string{strconv.FormatInt(ts.UnixNano(), 10), line.Body})
	}

	body := lokiPush{
		Streams: []lokiStream{{
			Stream: map[string]string{
				"job":     "watchtower-e2e",
				"run_id":  runID,
				"case_id": caseID,
				"stream":  streamName,
			},
			Values: values,
		}},
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal loki push: %w", err)
	}

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, l.base+"/loki/api/v1/push", bytes.NewReader(raw))
	if reqErr != nil {
		return fmt.Errorf("loki push request: %w", reqErr)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, doErr := l.client.Do(req)
	if doErr != nil {
		return fmt.Errorf("loki push: %w", doErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		slurp, readErr := io.ReadAll(io.LimitReader(resp.Body, lokiErrorBodyLimit))
		if readErr != nil {
			return fmt.Errorf("%w: push status %d: %v", ErrHTTP, resp.StatusCode, readErr)
		}

		return fmt.Errorf("%w: push status %d: %s", ErrHTTP, resp.StatusCode, slurp)
	}

	return nil
}
