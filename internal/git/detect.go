package git

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/nicholas-fedor/watchtower/internal/git/provider"
)

const probeBodyLimit = 64 << 10

// knownAPI is a confirmed REST fingerprint for one hosted Git product.
type knownAPI struct {
	match func(apiProbe) bool
	kind  string
	paths [][]string
}

// apiProbe is the closed HTTP result of one fingerprint request.
type apiProbe struct {
	Header http.Header
	Body   []byte
	Status int
}

// knownAPIs is the canonical map of product → version/metadata endpoints.
// A host is classified only when a probe response matches that product's
// payload or headers. Status codes alone are not enough.
var knownAPIs = []knownAPI{
	{
		kind:  provider.Gitea,
		paths: [][]string{{"api", "v1", "version"}},
		match: matchGiteaAPI,
	},
	{
		kind:  provider.GitLab,
		paths: [][]string{{"api", "v4", "metadata"}, {"api", "v4", "version"}},
		match: matchGitLabAPI,
	},
	{
		kind:  provider.GitHub,
		paths: [][]string{{"api", "v3"}},
		match: matchGitHubAPI,
	},
}

// detectKind probes known product APIs until one fingerprint matches.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - origin: HTTP origin from git-host.
//   - cloneHost: Clone hostname. Only a probe of that host carries credentials.
//
// Returns:
//   - string: Product kind, or empty when no API matches.
func (c *Client) detectKind(ctx context.Context, origin url.URL, cloneHost string) string {
	for _, spec := range knownAPIs {
		for _, segments := range spec.paths {
			endpoint := origin.JoinPath(segments...)

			probe, err := c.probeAPI(ctx, endpoint, cloneHost)
			if err != nil {
				continue
			}

			// 401/403 only means the path exists if the body or headers match.
			if !probeUsable(probe.Status) {
				continue
			}

			if spec.match(probe) {
				return spec.kind
			}
		}
	}

	return ""
}

// probeAPI GETs endpoint and returns a bounded JSON-oriented response.
//
// Parameters:
//   - ctx: Cancellation and timeout.
//   - endpoint: Fully built probe URL.
//   - cloneHost: Clone hostname. Only a probe of that host carries credentials.
//
// Returns:
//   - apiProbe: Status, headers, and truncated body.
//   - error: Non-nil on request or read failure.
func (c *Client) probeAPI(ctx context.Context, endpoint *url.URL, cloneHost string) (apiProbe, error) {
	if endpoint == nil || endpoint.Host == "" {
		return apiProbe{}, errEmptyProbeURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return apiProbe{}, fmt.Errorf("new request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	c.applyAuth(req, cloneHost)

	resp, err := c.http.Do(req)
	if err != nil {
		return apiProbe{}, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, probeBodyLimit))
	if err != nil {
		return apiProbe{}, fmt.Errorf("read body: %w", err)
	}

	return apiProbe{
		Status: resp.StatusCode,
		Header: resp.Header.Clone(),
		Body:   body,
	}, nil
}

// probeUsable reports whether status is worth fingerprinting.
//
// Parameters:
//   - status: HTTP status code.
//
// Returns:
//   - bool: True for 200, 401, or 403.
func probeUsable(status int) bool {
	switch status {
	case http.StatusOK, http.StatusUnauthorized, http.StatusForbidden:
		return true
	default:
		return false
	}
}

// matchGiteaAPI reports whether the body is a Gitea or Forgejo version payload.
//
// Parameters:
//   - probe: HTTP probe result.
//
// Returns:
//   - bool: True when a non-empty version field is present.
func matchGiteaAPI(probe apiProbe) bool {
	var payload struct {
		Version string `json:"version"`
	}

	if json.Unmarshal(probe.Body, &payload) != nil {
		return false
	}

	return payload.Version != ""
}

// matchGitLabAPI reports whether the body is GitLab metadata or version JSON.
//
// Parameters:
//   - probe: HTTP probe result.
//
// Returns:
//   - bool: True when version is set and revision or enterprise is present.
func matchGitLabAPI(probe apiProbe) bool {
	var payload struct {
		Version    string `json:"version"`
		Revision   string `json:"revision"`
		Enterprise *bool  `json:"enterprise"`
	}

	if json.Unmarshal(probe.Body, &payload) != nil {
		return false
	}

	return payload.Version != "" && (payload.Revision != "" || payload.Enterprise != nil)
}

// matchGitHubAPI reports whether headers or body identify GitHub Enterprise.
//
// Parameters:
//   - probe: HTTP probe result.
//
// Returns:
//   - bool: True when GitHub request headers or current_user_url are present.
func matchGitHubAPI(probe apiProbe) bool {
	// Canonical MIME header form is X-Github-Request-Id.
	if probe.Header.Get("X-Github-Request-Id") != "" ||
		probe.Header.Get("X-Github-Enterprise-Version") != "" {
		return true
	}

	var payload struct {
		CurrentUserURL string `json:"current_user_url"`
	}

	return json.Unmarshal(probe.Body, &payload) == nil && payload.CurrentUserURL != ""
}
