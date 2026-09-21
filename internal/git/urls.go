package git

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errCrossHostRedirect indicates a Location header pointed at another origin.
var errCrossHostRedirect = errors.New("git http redirect left the original host")

// applyAuth sets Bearer or basic credentials on req.
//
// Credentials are sent only for HTTPS, and only when the request hostname
// is the clone host or GitHub's api.github.com for a github.com clone.
// A container git-host label cannot redirect the process token.
//
// Parameters:
//   - req: Outgoing HTTP request. Ignored when nil.
//   - cloneHost: Hostname from the clone URL. Empty sends no credentials.
//
// Returns:
//   - none.
func (c *Client) applyAuth(req *http.Request, cloneHost string) {
	if c == nil || req == nil || req.URL == nil || req.URL.Scheme != "https" {
		return
	}

	if !credentialHostAllowed(req.URL.Hostname(), cloneHost) {
		return
	}

	if c.opts.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.opts.Token)

		return
	}

	if c.opts.Username != "" || c.opts.Password != "" {
		req.SetBasicAuth(c.opts.Username, c.opts.Password)
	}
}

// credentialHostAllowed reports whether a process credential may be sent to requestHost.
//
// Parameters:
//   - requestHost: Hostname of the outgoing request, port ignored.
//   - cloneHost: Hostname from the clone URL, port ignored.
//
// Returns:
//   - bool: True when the hosts match, or the request is api.github.com for github.com.
func credentialHostAllowed(requestHost, cloneHost string) bool {
	requestHost = toHostname(requestHost)
	cloneHost = toHostname(cloneHost)

	if requestHost == "" || cloneHost == "" {
		return false
	}

	if requestHost == cloneHost {
		return true
	}

	return cloneHost == "github.com" && requestHost == "api.github.com"
}

// originOf returns the classified HTTP origin for host.
//
// Parameters:
//   - host: Hostname, optionally with brackets.
//
// Returns:
//   - url.URL: Stored origin, or https://host.
func (c *Client) originOf(host string) url.URL {
	host = toHostname(host)
	if c != nil && c.origins != nil {
		if origin, ok := c.origins[host]; ok {
			return origin
		}
	}

	return url.URL{Scheme: "https", Host: httpHost(host)}
}

// httpHost returns hostname, bracketed when it is IPv6.
//
// Parameters:
//   - hostname: Host without port.
//
// Returns:
//   - string: Host suitable for url.URL.Host without a port.
func httpHost(hostname string) string {
	if strings.Contains(hostname, ":") {
		return "[" + hostname + "]"
	}

	return hostname
}

// githubAPI builds a GitHub REST URL under api.github.com or host/api/v3.
//
// Parameters:
//   - host: github.com or a GitHub Enterprise hostname.
//   - segments: Path segments after the API root.
//
// Returns:
//   - *url.URL: Absolute API URL.
func (c *Client) githubAPI(host string, segments ...string) *url.URL {
	return c.githubAPIAt(c.originOf(host), host, segments...)
}

// githubAPIAt builds a GitHub REST URL from an explicit API origin.
//
// Parameters:
//   - origin: HTTP API origin from a git-host label, or originOf(host).
//   - host: Clone hostname. github.com always uses api.github.com.
//   - segments: Path segments after the API root.
//
// Returns:
//   - *url.URL: Absolute API URL.
func (c *Client) githubAPIAt(origin url.URL, host string, segments ...string) *url.URL {
	if strings.EqualFold(toHostname(host), "github.com") &&
		(origin.Host == "" || strings.EqualFold(origin.Hostname(), "github.com")) {
		return (&url.URL{Scheme: "https", Host: "api.github.com"}).JoinPath(segments...)
	}

	if origin.Host == "" {
		origin = c.originOf(host)
	}

	return origin.JoinPath(append([]string{"api", "v3"}, segments...)...)
}

// gitlabAPI builds a GitLab REST URL under host/api/v4.
//
// Parameters:
//   - host: GitLab hostname.
//   - segments: Path segments after the API root.
//
// Returns:
//   - *url.URL: Absolute API URL.
func (c *Client) gitlabAPI(host string, segments ...string) *url.URL {
	return c.gitlabAPIAt(c.originOf(host), host, segments...)
}

// gitlabAPIAt builds a GitLab REST URL from an explicit API origin.
//
// Parameters:
//   - origin: HTTP API origin from a git-host label, or originOf(host).
//   - host: Clone hostname, used when origin is empty.
//   - segments: Path segments after the API root.
//
// Returns:
//   - *url.URL: Absolute API URL.
func (c *Client) gitlabAPIAt(origin url.URL, host string, segments ...string) *url.URL {
	if origin.Host == "" {
		origin = c.originOf(host)
	}

	return origin.JoinPath(append([]string{"api", "v4"}, segments...)...)
}

// giteaAPI builds a Gitea or Forgejo REST URL under host/api/v1.
//
// Parameters:
//   - host: Gitea or Forgejo hostname.
//   - segments: Path segments after the API root.
//
// Returns:
//   - *url.URL: Absolute API URL.
func (c *Client) giteaAPI(host string, segments ...string) *url.URL {
	return c.giteaAPIAt(c.originOf(host), host, segments...)
}

// giteaAPIAt builds a Gitea or Forgejo REST URL from an explicit API origin.
//
// Parameters:
//   - origin: HTTP API origin from a git-host label, or originOf(host).
//   - host: Clone hostname, used when origin is empty.
//   - segments: Path segments after the API root.
//
// Returns:
//   - *url.URL: Absolute API URL.
func (c *Client) giteaAPIAt(origin url.URL, host string, segments ...string) *url.URL {
	if origin.Host == "" {
		origin = c.originOf(host)
	}

	return origin.JoinPath(append([]string{"api", "v1"}, segments...)...)
}

// newGitHTTPClient builds an HTTP client for REST probes and API ref checks.
//
// Parameters:
//   - timeout: Client timeout.
//   - insecure: When true, skip TLS verification.
//   - caPEM: Optional PEM CA bundle.
//
// Returns:
//   - *http.Client: Client with optional custom TLS.
func newGitHTTPClient(timeout time.Duration, insecure bool, caPEM []byte) *http.Client {
	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: rejectCrossHostRedirect,
	}
	if !insecure && len(caPEM) == 0 {
		return client
	}

	base, ok := http.DefaultTransport.(*http.Transport)

	var transport *http.Transport
	if ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		// Operator opt-in via git-insecure-skip-tls for private CAs that cannot be bundled.
		cfg.InsecureSkipVerify = true
	}

	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM(caPEM) {
			cfg.RootCAs = pool
		}
	}

	transport.TLSClientConfig = cfg
	client.Transport = transport

	return client
}

// toHostname lowercases host and strips IPv6 brackets.
//
// Parameters:
//   - host: Hostname or [IPv6] literal.
//
// Returns:
//   - string: Lowercase hostname.
func toHostname(host string) string {
	return strings.ToLower(strings.Trim(host, "[]"))
}

// joinEscaped appends path segments onto base, escaping each segment.
//
// Use this when a segment may contain slashes, such as GitLab project IDs.
//
// Parameters:
//   - base: API origin.
//   - segments: Path segments to escape and join.
//
// Returns:
//   - *url.URL: Joined URL. Empty when base is nil.
func joinEscaped(base *url.URL, segments ...string) *url.URL {
	if base == nil {
		return &url.URL{}
	}

	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}

	raw := strings.TrimSuffix(base.String(), "/") + "/" + strings.Join(escaped, "/")

	parsed, err := url.Parse(raw)
	if err != nil {
		return base.JoinPath(segments...)
	}

	return parsed
}

// rejectCrossHostRedirect blocks redirects that change scheme or host.
//
// Git API calls carry Authorization. Following a cross-origin Location would
// forward that token to an attacker-controlled host.
//
// Parameters:
//   - req: The request that would be issued for the redirect.
//   - via: Prior requests in the redirect chain.
//
// Returns:
//   - error: Non-nil when the redirect leaves the original origin.
func rejectCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}

	prev := via[len(via)-1]
	if req.URL == nil || prev.URL == nil {
		return errCrossHostRedirect
	}

	if !strings.EqualFold(req.URL.Scheme, prev.URL.Scheme) || !strings.EqualFold(req.URL.Host, prev.URL.Host) {
		return errCrossHostRedirect
	}

	return nil
}
