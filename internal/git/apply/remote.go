// Package apply implements Git apply backends after monitoring finds a stale ref.
package apply

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

const (
	httpPort  = 80
	httpsPort = 443
	sshPort   = 22
)

var (
	// ErrRemoteEmpty indicates no clone URL was associated.
	ErrRemoteEmpty = errors.New("git remote is empty")
	// ErrRemoteInvalid indicates the clone URL cannot be turned into an HTTPS Git context.
	ErrRemoteInvalid = errors.New("invalid git remote")
	// ErrCommitEmpty indicates monitoring produced no commit SHA.
	ErrCommitEmpty = errors.New("git commit is empty")
	// ErrCommitInvalid indicates the commit cannot be used in a Docker Git fragment.
	ErrCommitInvalid = errors.New("invalid git commit")
	// ErrNeedsHTTPS indicates a token was given for a non-HTTP Git context.
	ErrNeedsHTTPS = errors.New("git build token requires an HTTPS remote")
	// ErrPathEscape indicates a context subdirectory left the repository.
	ErrPathEscape = errors.New("git build path escapes checkout")
)

// RemoteContext builds a Docker Git build-context URL.
//
// Format: {httpsURL}#{commit}[:{subdir}].
//
// Parameters:
//   - repo: Associated clone URL.
//   - commit: Commit SHA from Git monitoring.
//   - contextRel: Optional subdirectory of the repo used as the build context.
//
// Returns:
//   - string: Docker Git URL context.
//   - error: Non-nil when the repo, commit, or context is invalid.
func RemoteContext(repo, commit, contextRel string) (string, error) {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return "", ErrCommitEmpty
	}

	// Docker's fragment is #ref:dir. Only hex object IDs are accepted so a
	// hostile ref advertisement cannot inject path or tag characters.
	if !validCommit(commit) {
		return "", fmt.Errorf("%w: %s", ErrCommitInvalid, commit)
	}

	httpsURL, err := httpsRemote(repo)
	if err != nil {
		return "", err
	}

	subdir, err := SafeRelPath(contextRel)
	if err != nil {
		return "", err
	}

	if subdir == "" {
		return httpsURL + "#" + commit, nil
	}

	return httpsURL + "#" + commit + ":" + subdir, nil
}

// WithAuth embeds a token in an HTTPS Git context URL for the builder.
//
// Parameters:
//   - remote: Git context URL from RemoteContext.
//   - username: Basic-auth user (for example x-access-token).
//   - token: Token or password. Empty leaves remote unchanged.
//
// Returns:
//   - string: URL with userinfo, or the original remote.
//   - error: Non-nil when remote is not HTTP(S) or cannot be parsed.
func WithAuth(remote, username, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return remote, nil
	}

	parsed, err := url.Parse(remote)
	if err != nil {
		return "", fmt.Errorf("parse git remote: %w", err)
	}

	if parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: %s", ErrNeedsHTTPS, remote)
	}

	if username == "" {
		username = "git"
	}

	parsed.User = url.UserPassword(username, token)

	return parsed.String(), nil
}

// httpsRemote converts a clone URL to an HTTPS (or HTTP) remote without userinfo.
//
// Parameters:
//   - repo: HTTPS, SSH, or scp-like Git URL.
//
// Returns:
//   - string: Scheme, host, and path suitable for Docker's remote context.
//   - error: Non-nil when the URL has no host or path.
func httpsRemote(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", ErrRemoteEmpty
	}

	endpoint, err := transport.NewEndpoint(repo)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrRemoteInvalid, repo)
	}

	host := strings.Trim(endpoint.Host, "[]")
	if host == "" {
		return "", fmt.Errorf("%w: %s", ErrRemoteInvalid, repo)
	}

	repoPath := strings.TrimPrefix(endpoint.Path, "/")
	if repoPath == "" {
		return "", fmt.Errorf("%w: %s", ErrRemoteInvalid, repo)
	}

	if !strings.HasSuffix(repoPath, ".git") {
		repoPath += ".git"
	}

	switch endpoint.Protocol {
	case "http", "https", "ssh", "git":
	default:
		return "", fmt.Errorf("%w: %s", ErrRemoteInvalid, repo)
	}

	// Preserve http only when the operator used it. Everything else becomes https.
	scheme := "https"
	if endpoint.Protocol == "http" {
		scheme = "http"
	}

	parsed := &url.URL{
		Scheme: scheme,
		Host:   httpHost(host),
		Path:   "/" + repoPath,
	}

	if keepPort(endpoint.Protocol, endpoint.Port) {
		parsed.Host = net.JoinHostPort(host, strconv.Itoa(endpoint.Port))
	}

	return parsed.String(), nil
}

// SafeRelPath confines a Dockerfile or context path to a relative repo path.
//
// Parameters:
//   - rel: Requested relative path from a label, flag, or Docker context subdir.
//
// Returns:
//   - string: Clean relative path, or empty for the repository root.
//   - error: Non-nil when the path would escape the repository.
func SafeRelPath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return "", nil
	}

	if path.IsAbs(rel) || strings.Contains(rel, ":") {
		return "", fmt.Errorf("%w: %s", ErrPathEscape, rel)
	}

	cleaned := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	if path.IsAbs(cleaned) || !filepath.IsLocal(filepath.FromSlash(cleaned)) {
		return "", fmt.Errorf("%w: %s", ErrPathEscape, rel)
	}

	return cleaned, nil
}

// validCommit reports whether commit is a hex Git object ID.
//
// Parameters:
//   - commit: Candidate commit from monitoring.
//
// Returns:
//   - bool: True when commit is 6–64 hex characters.
func validCommit(commit string) bool {
	n := len(commit)
	if n < 6 || n > 64 {
		return false
	}

	for i := range n {
		c := commit[i]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' {
			continue
		}

		return false
	}

	return true
}

// keepPort reports whether port should appear on an HTTPS Git context host.
//
// SSH's default port 22 is dropped when converting to HTTPS.
//
// Parameters:
//   - protocol: Original transport protocol from go-git.
//   - port: Endpoint port, or 0 when unset.
//
// Returns:
//   - bool: True when the port is non-default for the target URL.
func keepPort(protocol string, port int) bool {
	if port == 0 {
		return false
	}

	switch protocol {
	case "http":
		return port != httpPort
	case "https":
		return port != httpsPort
	default:
		return port != sshPort
	}
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
