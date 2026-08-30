package stream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// lokiTimeout is the HTTP deadline for Loki push, query, and ready.
	lokiTimeout = 10 * time.Second
	// lokiErrorBodyLimit is how many error-response bytes to keep.
	lokiErrorBodyLimit = 2048
	// lokiQueryLimit is the max lines requested from query_range.
	lokiQueryLimit = "5000"
	// lokiLookback is how far Query looks back.
	lokiLookback = 168 * time.Hour
)

// Loki pushes and queries Grafana Loki over HTTP. No Docker plugin.
type Loki struct {
	// base is the Loki HTTP origin without a trailing slash.
	base string
	// client is the HTTP transport.
	client *http.Client
}

// ErrHTTP is a Loki HTTP status that is not success.
var ErrHTTP = errors.New("loki http")

// lokiPush is the Loki ingest document.
type lokiPush struct {
	Streams []lokiStream `json:"streams"`
}

// lokiStream is one labeled stream in a push.
type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

// lokiQuery is the Loki query_range response.
type lokiQuery struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// OpenLoki returns a Loki client for the given base URL (e.g. http://127.0.0.1:3100).
//
// Parameters:
//   - baseURL: Loki HTTP origin.
//
// Returns:
//   - *Loki: Client.
func OpenLoki(baseURL string) *Loki {
	return &Loki{
		base:   strings.TrimRight(baseURL, "/"),
		client: &http.Client{Timeout: lokiTimeout},
	}
}

// Ready hits Loki /ready.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - error: HTTP failure.
func (l *Loki) Ready(ctx context.Context) error {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, l.base+"/ready", nil)
	if reqErr != nil {
		return fmt.Errorf("loki ready request: %w", reqErr)
	}

	resp, doErr := l.client.Do(req)
	if doErr != nil {
		return fmt.Errorf("loki ready: %w", doErr)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("%w: ready status %d", ErrHTTP, resp.StatusCode)
	}

	return nil
}

// Close is a no-op for the HTTP client.
//
// Returns:
//   - error: Always nil.
func (l *Loki) Close() error {
	return nil
}

// escapeLabel escapes backslash and quote for LogQL label values.
//
// Parameters:
//   - v: Raw label value.
//
// Returns:
//   - string: Escaped value.
func escapeLabel(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v)
}
