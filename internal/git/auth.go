package git

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"

	gitHTTP "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitSSH "github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"github.com/nicholas-fedor/watchtower/internal/git/provider"
)

const (
	tokenUserGitHub = "x-access-token"
	tokenUserGitLab = "oauth2"
	tokenUserGitea  = "gitea"
	tokenUserGit    = "git"
)

// TLSSettings returns the process-wide Git HTTPS TLS settings.
//
// Returns:
//   - []byte: PEM CA bundle, or nil.
//   - bool: True when TLS verification is skipped.
func (c *Client) TLSSettings() ([]byte, bool) {
	if c == nil {
		return nil, false
	}

	return c.opts.CABundle, c.opts.InsecureSkipTLS
}

// BuildAuth returns HTTPS credentials the Docker builder can embed in remote.
//
// Credentials are returned only when remote is an HTTPS URL with a host, and
// they are for that host. The Git context URL is HTTPS for HTTPS, SSH and git
// clone URLs, and plaintext HTTP for an http clone URL.
//
// Parameters:
//   - remote: Docker Git context URL that the credentials will be embedded in.
//
// Returns:
//   - username: Basic user for the token or configured username.
//   - token: Token or password. Empty when the builder should clone anonymously.
func (c *Client) BuildAuth(remote string) (string, string) {
	if c == nil || !httpsRemote(remote) {
		return tokenUserGit, ""
	}

	if c.opts.Token != "" {
		return tokenUsername(remote, c.opts.Hosts), c.opts.Token
	}

	if c.opts.Password != "" {
		username := c.opts.Username
		if username == "" {
			username = tokenUserGit
		}

		return username, c.opts.Password
	}

	return tokenUserGit, ""
}

// Auth returns the process-wide go-git auth method for repo, which matches
// the remote URL scheme.
//
// Process credentials go only to the host repo names, over HTTPS or SSH. A nil
// client gets none. SSH and scp-like remotes use only the SSH key, when one is
// set. HTTPS uses the token, then the username and password. Every other case
// gets no process credentials.
//
// Parameters:
//   - repo: Clone or origin URL used to pick the scheme and token username.
//
// Returns:
//   - transport.AuthMethod: Auth for go-git, or nil without process credentials.
//   - error: Non-nil when an SSH key or known_hosts file cannot be loaded.
func (c *Client) Auth(repo string) (transport.AuthMethod, error) {
	switch {
	case c == nil:
		// A missing client gets no process credentials.
	case sshRemote(repo) && c.opts.SSHKeyPath != "":
		return c.sshAuth()
	case !httpsRemote(repo):
		// SSH without a key, plaintext HTTP and every other scheme get no
		// process credentials.
	case c.opts.Token != "":
		return &gitHTTP.BasicAuth{
			Username: tokenUsername(repo, c.opts.Hosts),
			Password: c.opts.Token,
		}, nil
	case c.opts.Username != "" || c.opts.Password != "":
		return &gitHTTP.BasicAuth{
			Username: c.opts.Username,
			Password: c.opts.Password,
		}, nil
	default:
		// No HTTPS credentials are configured.
	}

	// With a nil AuthMethod, go-git uses credentials written into an HTTP
	// clone URL and the SSH agent for an SSH remote. A non-nil value would
	// replace them.
	//nolint:nilnil // A nil AuthMethod carries no process credentials.
	return nil, nil
}

// sshAuth loads the configured SSH key.
//
// Returns:
//   - transport.AuthMethod: PublicKeys auth for the key.
//   - error: Non-nil when the key or known_hosts file cannot be loaded.
func (c *Client) sshAuth() (transport.AuthMethod, error) {
	publicKeys, err := gitSSH.NewPublicKeysFromFile(tokenUserGit, c.opts.SSHKeyPath, "")
	if err != nil {
		return nil, fmt.Errorf("load ssh key: %w", err)
	}

	if c.opts.SSHKnownHosts != "" {
		callback, err := gitSSH.NewKnownHostsCallback(c.opts.SSHKnownHosts)
		if err != nil {
			return nil, fmt.Errorf("load ssh known_hosts: %w", err)
		}

		publicKeys.HostKeyCallback = callback
	}

	return publicKeys, nil
}

// httpsRemote reports whether repo is an HTTPS Git URL with a host.
//
// Parameters:
//   - repo: Clone or origin URL.
//
// Returns:
//   - bool: True for https:// remotes that name a host.
func httpsRemote(repo string) bool {
	endpoint, err := transport.NewEndpoint(repo)

	return err == nil && endpoint.Protocol == "https" && endpoint.Host != ""
}

// sshRemote reports whether repo is an SSH or scp-like Git URL with a host.
//
// Parameters:
//   - repo: Clone or origin URL.
//
// Returns:
//   - bool: True for ssh:// and git@host:path remotes that name a host.
func sshRemote(repo string) bool {
	endpoint, err := transport.NewEndpoint(repo)

	return err == nil && endpoint.Protocol == "ssh" && endpoint.Host != ""
}

// tokenUsername returns the Basic-auth user for a hosted Git token.
//
// Parameters:
//   - repo: Clone URL.
//   - extra: Extra hostname to product mappings.
//
// Returns:
//   - string: Product-specific username, or git when the host is unknown.
func tokenUsername(repo string, extra map[string]string) string {
	host, _, _, ok := splitRepo(repo)
	if !ok {
		return tokenUserGit
	}

	switch provider.ResolveHostKind(host, extra) {
	case provider.GitHub:
		return tokenUserGitHub
	case provider.GitLab:
		return tokenUserGitLab
	case provider.Gitea:
		return tokenUserGitea
	default:
		return tokenUserGit
	}
}
