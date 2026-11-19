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

// BuildAuth returns HTTPS credentials the Docker builder can embed in a Git context URL.
//
// Credentials are returned only for a trusted host that is not plaintext HTTP.
// A container label cannot redirect the process token or password to another host.
//
// Parameters:
//   - repo: Associated clone URL.
//
// Returns:
//   - username: Basic user for the token or configured username.
//   - token: Token or password. Empty when the builder should clone anonymously.
func (c *Client) BuildAuth(repo string) (string, string) {
	if c == nil || !c.builderCredentialAllowed(repo) {
		return tokenUserGit, ""
	}

	if c.opts.Token != "" {
		return tokenUsername(repo, c.opts.Hosts), c.opts.Token
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
// A nil client and an untrusted host get no process credentials. SSH and
// scp-like remotes use only the SSH key, when one is set. HTTPS uses the token,
// then the username and password. Plaintext HTTP never receives credentials.
// Every other case gets no process credentials.
//
// Parameters:
//   - repo: Clone or origin URL used to pick the scheme and token username.
//
// Returns:
//   - transport.AuthMethod: Auth for go-git, or nil without process credentials.
//   - error: Non-nil when an SSH key or known_hosts file cannot be loaded.
func (c *Client) Auth(repo string) (transport.AuthMethod, error) {
	switch {
	case c == nil || !c.credentialHostTrusted(repo):
		// A missing client or an untrusted host gets no process credentials.
	case sshRemote(repo) && c.opts.SSHKeyPath != "":
		return c.sshAuth()
	case !httpsRemote(repo):
		// SSH without a key and plaintext HTTP get no process credentials.
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

// builderCredentialAllowed reports whether BuildAuth may embed process credentials.
//
// SSH clone URLs are allowed because the builder context is rewritten to HTTPS.
// Plaintext HTTP is not.
//
// Parameters:
//   - repo: Associated clone URL.
//
// Returns:
//   - bool: True when the host is trusted and the URL is not plaintext HTTP.
func (c *Client) builderCredentialAllowed(repo string) bool {
	if c == nil || !c.credentialHostTrusted(repo) || plaintextHTTP(repo) {
		return false
	}

	return true
}

// credentialHostTrusted reports whether repo's host may receive process credentials.
//
// Built-in provider hosts and operator host mappings are trusted. A container
// label cannot add a host.
//
// Parameters:
//   - repo: Clone or origin URL.
//
// Returns:
//   - bool: True when the hostname is a known or configured provider host.
func (c *Client) credentialHostTrusted(repo string) bool {
	if c == nil {
		return false
	}

	host, _, _, ok := splitRepo(repo)
	if !ok || host == "" {
		return false
	}

	return provider.ResolveHostKind(host, c.opts.Hosts) != ""
}

// httpsRemote reports whether repo is an HTTPS Git URL.
//
// Parameters:
//   - repo: Clone or origin URL.
//
// Returns:
//   - bool: True for https:// remotes.
func httpsRemote(repo string) bool {
	endpoint, err := transport.NewEndpoint(repo)

	return err == nil && endpoint.Protocol == "https"
}

// plaintextHTTP reports whether repo is a plaintext HTTP Git URL.
//
// Parameters:
//   - repo: Clone or origin URL.
//
// Returns:
//   - bool: True for http:// remotes.
func plaintextHTTP(repo string) bool {
	endpoint, err := transport.NewEndpoint(repo)

	return err == nil && endpoint.Protocol == "http"
}

// sshRemote reports whether repo is an SSH or scp-like Git URL.
//
// Parameters:
//   - repo: Clone or origin URL.
//
// Returns:
//   - bool: True for ssh:// and git@host:path remotes.
func sshRemote(repo string) bool {
	endpoint, err := transport.NewEndpoint(repo)
	if err != nil {
		return false
	}

	return endpoint.Protocol == "ssh"
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
