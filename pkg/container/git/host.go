package git

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrInvalidHost indicates a git-host label is not an HTTP base URL.
var ErrInvalidHost = errors.New("invalid git-host")

// ParseAPIOrigin parses a git-host label as an HTTP API base URL.
//
// The value is the server only, for example https://git.example.com:3000.
// A repository path is rejected. host=type mappings are rejected.
//
// Parameters:
//   - raw: Label value.
//
// Returns:
//   - url.URL: Scheme, host[:port], and optional path prefix.
//   - error: Non-nil when the value is empty or not an HTTP URL.
func ParseAPIOrigin(raw string) (url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return url.URL{}, fmt.Errorf("%w: empty value", ErrInvalidHost)
	}

	if strings.Contains(raw, "=") {
		return url.URL{}, fmt.Errorf("%w: host=type mappings are not supported", ErrInvalidHost)
	}

	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return url.URL{}, fmt.Errorf("%w: malformed URL", ErrInvalidHost)
	}

	if parsed.Hostname() == "" {
		return url.URL{}, fmt.Errorf("%w: missing hostname", ErrInvalidHost)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return url.URL{}, fmt.Errorf("%w: scheme must be http or https", ErrInvalidHost)
	}

	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.Host = canonicalAPIHost(strings.ToLower(parsed.Hostname()), parsed.Port())

	return *parsed, nil
}

// canonicalAPIHost builds a url.URL Host value from a hostname and optional port.
//
// Parameters:
//   - hostname: Lowercase hostname, without brackets.
//   - port: Port string, or empty.
//
// Returns:
//   - string: Host or host:port, with IPv6 brackets when needed.
func canonicalAPIHost(hostname, port string) string {
	if port != "" {
		return net.JoinHostPort(hostname, port)
	}

	if strings.Contains(hostname, ":") {
		return "[" + hostname + "]"
	}

	return hostname
}
