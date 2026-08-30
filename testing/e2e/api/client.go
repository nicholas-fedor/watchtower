package api

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nicholas-fedor/watchtower/testing/e2e/infra"
)

const (
	// clientTimeout is the HTTP deadline for control-plane calls.
	clientTimeout = 30 * time.Second
	// errorBodyLimit is how many error-response bytes to keep.
	errorBodyLimit = 2048
	// waitTick is how often Wait rereads a sitting.
	waitTick = 500 * time.Millisecond
)

// ErrStatus means the JSON API returned a non-success status.
var ErrStatus = errors.New("control plane request failed")

// Client talks to the control-plane JSON API.
type Client struct {
	// base is the origin, including scheme.
	base string
	// token is an optional bearer. Empty skips the header.
	token string
	// http is the HTTP transport.
	http *http.Client
}

// NewClient builds a client from WATCHTOWER_E2E_* env defaults.
//
// Returns:
//   - *Client: Ready client.
func NewClient() *Client {
	env := infra.FromEnv()

	return NewClientAt("http://"+env.Listen, env.Token)
}

// NewClientAt talks to an explicit origin.
//
// Parameters:
//   - base: Origin including scheme, such as http://127.0.0.1:9472.
//   - token: Optional bearer. Empty skips the header.
//
// Returns:
//   - *Client: Ready client.
func NewClientAt(base, token string) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		http:  &http.Client{Timeout: clientTimeout},
	}
}

// UseHTTP replaces the transport. Tests pass httptest.Server.Client().
//
// Parameters:
//   - h: HTTP client. Nil is ignored.
func (c *Client) UseHTTP(h *http.Client) {
	if h != nil {
		c.http = h
	}
}

// Base returns the origin, including scheme.
//
// Returns:
//   - string: Origin such as http://127.0.0.1:9472.
func (c *Client) Base() string {
	return c.base
}

// Healthy reports whether GET /v1/health returns 200.
//
// Parameters:
//   - ctx: Cancellation.
//
// Returns:
//   - bool: True when the control plane answers 200.
func (c *Client) Healthy(ctx context.Context) bool {
	resp, err := c.do(ctx, http.MethodGet, "/v1/health", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// do issues one JSON request.
//
// Parameters:
//   - ctx: Cancellation.
//   - method: HTTP method.
//   - path: Path beginning with /v1.
//   - body: Value to marshal, or nil.
//
// Returns:
//   - *http.Response: Open response. Caller closes Body.
//   - error: Dial or marshal failure.
func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reader io.Reader

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}

		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("control plane request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	return c.http.Do(req)
}

// decodeJSON reads T from a JSON response body.
//
// Parameters:
//   - resp: HTTP response. Body must still be open.
//
// Returns:
//   - T: Decoded value.
//   - error: Non-success status or JSON failure.
func decodeJSON[T any](resp *http.Response) (T, error) {
	var zero T

	if resp.StatusCode >= 300 {
		slurp, readErr := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		if readErr != nil {
			return zero, fmt.Errorf("%w: %s: %v", ErrStatus, resp.Status, readErr)
		}

		return zero, fmt.Errorf("%w: %s %s", ErrStatus, resp.Status, strings.TrimSpace(string(slurp)))
	}

	var out T

	err := json.UnmarshalRead(resp.Body, &out)
	if err != nil {
		return zero, fmt.Errorf("decode body: %w", err)
	}

	return out, nil
}
