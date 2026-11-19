package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	gitHTTP "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitSSH "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// TestClient_Auth verifies which go-git auth method each remote receives.
// Credentials go to the clone URL's own host over SSH or HTTPS, including a
// self-hosted server.
func TestClient_Auth(t *testing.T) {
	t.Parallel()

	t.Run("token wins over basic and ssh", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			Token:      "tok",
			Username:   "user",
			Password:   "pw",
			SSHKeyPath: filepath.Join(t.TempDir(), "missing"),
		}}

		got, err := client.Auth("https://codeberg.org/org/app.git")
		require.NoError(t, err)

		basic, ok := got.(*gitHTTP.BasicAuth)
		require.True(t, ok)
		assert.Equal(t, tokenUserGitea, basic.Username)
		assert.Equal(t, "tok", basic.Password)
	})

	t.Run("github token username", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{Token: "ghp_xxx"}}
		got, err := client.Auth("https://github.com/org/app.git")
		require.NoError(t, err)

		basic, ok := got.(*gitHTTP.BasicAuth)
		require.True(t, ok)
		assert.Equal(t, tokenUserGitHub, basic.Username)
		assert.Equal(t, "ghp_xxx", basic.Password)
	})

	t.Run("basic username and password", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			Username: "octocat",
			Password: "pw",
		}}
		got, err := client.Auth("https://git.example.com/org/app.git")
		require.NoError(t, err)

		basic, ok := got.(*gitHTTP.BasicAuth)
		require.True(t, ok)
		assert.Equal(t, "octocat", basic.Username)
		assert.Equal(t, "pw", basic.Password)
	})

	t.Run("username only", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			Username: "octocat",
		}}
		got, err := client.Auth("https://git.example.com/org/app.git")
		require.NoError(t, err)

		basic, ok := got.(*gitHTTP.BasicAuth)
		require.True(t, ok)
		assert.Equal(t, "octocat", basic.Username)
		assert.Empty(t, basic.Password)
	})

	t.Run("password only", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			Password: "pw",
		}}
		got, err := client.Auth("https://git.example.com/org/app.git")
		require.NoError(t, err)

		basic, ok := got.(*gitHTTP.BasicAuth)
		require.True(t, ok)
		assert.Empty(t, basic.Username)
		assert.Equal(t, "pw", basic.Password)
	})

	t.Run("self-hosted https host receives the token", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{Token: "tok", Username: "octocat", Password: "pw"}}
		got, err := client.Auth("https://git.example.com/org/app.git")
		require.NoError(t, err)

		basic, ok := got.(*gitHTTP.BasicAuth)
		require.True(t, ok)
		assert.Equal(t, tokenUserGit, basic.Username)
		assert.Equal(t, "tok", basic.Password)
	})

	t.Run("self-hosted ssh key logs in as the url's user", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{SSHKeyPath: writeEd25519Key(t)}}

		for origin, wantUser := range map[string]string{
			"deploy@git.example.com:org/app.git":             "deploy",
			"ssh://forgejo@git.example.com:2222/org/app.git": "forgejo",
			"git.example.com:org/app.git":                    tokenUserGit,
		} {
			got, err := client.Auth(origin)
			require.NoError(t, err)

			keys, ok := got.(*gitSSH.PublicKeys)
			require.True(t, ok, origin)
			assert.Equal(t, wantUser, keys.User, origin)
		}
	})

	t.Run("self-hosted urls without an owner receive credentials", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{Token: "tok", SSHKeyPath: writeEd25519Key(t)}}

		got, err := client.Auth("https://git.example.com/app.git")
		require.NoError(t, err)

		basic, ok := got.(*gitHTTP.BasicAuth)
		require.True(t, ok)
		assert.Equal(t, "tok", basic.Password)

		got, err = client.Auth("git@git.example.com:app.git")
		require.NoError(t, err)

		_, ok = got.(*gitSSH.PublicKeys)
		assert.True(t, ok)
	})

	t.Run("urls without a host are anonymous", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{Token: "tok", SSHKeyPath: writeEd25519Key(t)}}

		for _, repo := range []string{
			"https:///org/app.git",
			"ssh:///org/app.git",
			"file:///srv/git/app.git",
		} {
			got, err := client.Auth(repo)
			require.NoError(t, err, repo)
			assertAnonymous(t, got)
		}
	})

	t.Run("plaintext http is anonymous", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			Username: "octocat",
			Password: "pw",
		}}
		got, err := client.Auth("http://git.example.com/org/app.git")
		require.NoError(t, err)
		assertAnonymous(t, got)
	})

	t.Run("nil client is anonymous", func(t *testing.T) {
		t.Parallel()

		got, err := (*Client)(nil).Auth("https://github.com/org/app.git")
		require.NoError(t, err)
		assertAnonymous(t, got)
	})

	t.Run("anonymous when no credentials", func(t *testing.T) {
		t.Parallel()

		got, err := (&Client{}).Auth("https://github.com/org/app.git")
		require.NoError(t, err)
		assertAnonymous(t, got)
	})

	t.Run("missing ssh key", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{SSHKeyPath: filepath.Join(t.TempDir(), "missing")}}
		_, err := client.Auth("git@github.com:org/app.git")
		require.Error(t, err)
		assert.ErrorContains(t, err, "load ssh key:")
	})

	t.Run("ssh key without known hosts", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{SSHKeyPath: writeEd25519Key(t)}}
		got, err := client.Auth("git@github.com:org/app.git")
		require.NoError(t, err)

		keys, ok := got.(*gitSSH.PublicKeys)
		require.True(t, ok)
		assert.Equal(t, tokenUserGit, keys.User)
		assert.Nil(t, keys.HostKeyCallback)
	})

	t.Run("ssh key with known hosts", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			SSHKeyPath:    writeEd25519Key(t),
			SSHKnownHosts: writeKnownHosts(t, "github.com"),
		}}
		got, err := client.Auth("git@github.com:org/app.git")
		require.NoError(t, err)

		keys, ok := got.(*gitSSH.PublicKeys)
		require.True(t, ok)
		assert.Equal(t, tokenUserGit, keys.User)
		assert.NotNil(t, keys.HostKeyCallback)
	})

	t.Run("ssh origin uses key even when token is set", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			Token:      "tok",
			Username:   "user",
			Password:   "pw",
			SSHKeyPath: writeEd25519Key(t),
		}}

		got, err := client.Auth("git@github.com:org/app.git")
		require.NoError(t, err)

		keys, ok := got.(*gitSSH.PublicKeys)
		require.True(t, ok)
		assert.Equal(t, tokenUserGit, keys.User)
	})

	t.Run("ssh url uses key even when token is set", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			Token:      "tok",
			SSHKeyPath: writeEd25519Key(t),
		}}

		got, err := client.Auth("ssh://git@github.com/org/app.git")
		require.NoError(t, err)

		_, ok := got.(*gitSSH.PublicKeys)
		assert.True(t, ok)
	})

	t.Run("ssh origin with token and no key is anonymous", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{Token: "tok", Password: "pw"}}
		for _, origin := range []string{"git@github.com:org/app.git", "ssh://git@github.com/org/app.git"} {
			got, err := client.Auth(origin)
			require.NoError(t, err)
			assertAnonymous(t, got)
		}
	})

	t.Run("self-hosted ssh host uses the key", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{Token: "tok", SSHKeyPath: writeEd25519Key(t)}}
		got, err := client.Auth("git@git.example.com:org/app.git")
		require.NoError(t, err)

		keys, ok := got.(*gitSSH.PublicKeys)
		require.True(t, ok)
		assert.Equal(t, tokenUserGit, keys.User)
	})

	t.Run("https with only an ssh key is anonymous", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{SSHKeyPath: writeEd25519Key(t)}}
		got, err := client.Auth("https://github.com/org/app.git")
		require.NoError(t, err)
		assertAnonymous(t, got)
	})

	t.Run("missing known hosts", func(t *testing.T) {
		t.Parallel()

		client := &Client{opts: Options{
			SSHKeyPath:    writeEd25519Key(t),
			SSHKnownHosts: filepath.Join(t.TempDir(), "missing_hosts"),
		}}
		_, err := client.Auth("git@github.com:org/app.git")
		require.Error(t, err)
		assert.ErrorContains(t, err, "load ssh known_hosts:")
	})
}

// TestClient_TLSSettings verifies that the client returns its CA bundle and
// skip-verify setting, and that a nil client returns neither.
func TestClient_TLSSettings(t *testing.T) {
	t.Parallel()

	bundle := []byte("-----BEGIN CERTIFICATE-----\n")
	client := &Client{opts: Options{CABundle: bundle, InsecureSkipTLS: true}}
	got, insecure := client.TLSSettings()
	assert.Equal(t, bundle, got)
	assert.True(t, insecure)

	got, insecure = (*Client)(nil).TLSSettings()
	assert.Nil(t, got)
	assert.False(t, insecure)
}

// TestBuildAuth verifies which credentials BuildAuth returns for a Docker Git
// context URL. Only an HTTPS context URL with a host receives them.
func TestBuildAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		client   *Client
		remote   string
		username string
		token    string
	}{
		{
			name:     "nil client is anonymous git",
			remote:   "https://github.com/org/app.git",
			username: tokenUserGit,
		},
		{
			name:     "empty client is anonymous git",
			client:   &Client{},
			remote:   "https://github.com/org/app.git",
			username: tokenUserGit,
		},
		{
			name: "token uses host username",
			client: &Client{opts: Options{
				Token:    "tok",
				Username: "ignored",
				Password: "ignored",
			}},
			remote:   "https://github.com/org/app.git",
			username: tokenUserGitHub,
			token:    "tok",
		},
		{
			name:     "token on codeberg uses the gitea username",
			client:   &Client{opts: Options{Token: "tok"}},
			remote:   "https://codeberg.org/org/app.git",
			username: tokenUserGitea,
			token:    "tok",
		},
		{
			name: "password with username",
			client: &Client{opts: Options{
				Username: "octocat",
				Password: "pw",
			}},
			remote:   "https://git.example.com/org/app.git",
			username: "octocat",
			token:    "pw",
		},
		{
			name: "password without username defaults to git",
			client: &Client{opts: Options{
				Password: "pw",
			}},
			remote:   "https://git.example.com/org/app.git",
			username: tokenUserGit,
			token:    "pw",
		},
		{
			name:     "self-hosted host receives the token",
			client:   &Client{opts: Options{Token: "tok", Password: "pw"}},
			remote:   "https://git.example.com/org/app.git",
			username: tokenUserGit,
			token:    "tok",
		},
		{
			name:     "context url with a commit and subdirectory receives the token",
			client:   &Client{opts: Options{Token: "tok"}},
			remote:   "https://git.example.com/org/app.git#abc123:apps/api",
			username: tokenUserGit,
			token:    "tok",
		},
		{
			name:     "ssh url is not a build context and is anonymous",
			client:   &Client{opts: Options{Token: "tok"}},
			remote:   "git@git.example.com:org/app.git",
			username: tokenUserGit,
		},
		{
			name:     "https url without a host is anonymous",
			client:   &Client{opts: Options{Token: "tok", Password: "pw"}},
			remote:   "https:///org/app.git",
			username: tokenUserGit,
		},
		{
			name:     "ssh url without a host is anonymous",
			client:   &Client{opts: Options{Token: "tok", Password: "pw"}},
			remote:   "ssh:///org/app.git",
			username: tokenUserGit,
		},
		{
			name:     "file url is anonymous",
			client:   &Client{opts: Options{Token: "tok", Password: "pw"}},
			remote:   "file:///srv/git/app.git",
			username: tokenUserGit,
		},
		{
			name:     "git protocol url is not a build context and is anonymous",
			client:   &Client{opts: Options{Token: "tok", Password: "pw"}},
			remote:   "git://git.example.com/org/app.git",
			username: tokenUserGit,
		},
		{
			name:     "self-hosted url without an owner receives the token",
			client:   &Client{opts: Options{Token: "tok"}},
			remote:   "https://git.example.com/app.git",
			username: tokenUserGit,
			token:    "tok",
		},
		{
			name: "plaintext http is anonymous",
			client: &Client{opts: Options{
				Token: "tok",
			}},
			remote:   "http://git.example.com/org/app.git",
			username: tokenUserGit,
		},
		{
			name:     "username without password is anonymous",
			client:   &Client{opts: Options{Username: "octocat"}},
			remote:   "https://github.com/org/app.git",
			username: tokenUserGit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			username, token := tt.client.BuildAuth(tt.remote)
			assert.Equal(t, tt.username, username)
			assert.Equal(t, tt.token, token)
		})
	}
}

func TestTokenUsername(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo string
		want string
	}{
		{name: "github.com", repo: "https://github.com/org/app.git", want: tokenUserGitHub},
		{name: "gitlab.com", repo: "https://gitlab.com/org/app.git", want: tokenUserGitLab},
		{name: "codeberg.org", repo: "https://codeberg.org/org/app.git", want: tokenUserGitea},
		{name: "ssh github url", repo: "git@github.com:org/app.git", want: tokenUserGitHub},
		{name: "unknown host", repo: "https://git.unknown.example/org/app.git", want: tokenUserGit},
		{name: "unparseable repo", repo: "not-a-repo", want: tokenUserGit},
		{name: "empty repo", repo: "", want: tokenUserGit},
		{name: "host only", repo: "https://github.com", want: tokenUserGit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tokenUsername(tt.repo))
		})
	}
}

// assertAnonymous checks that got is a nil AuthMethod, which carries no
// process credentials.
//
// An interface holding a nil pointer is not nil, so got is compared with nil
// directly rather than through assert.Nil. go-git would use such a value in
// place of credentials written into the clone URL.
//
// Parameters:
//   - t: Test handle.
//   - got: The auth method Auth returned.
func assertAnonymous(t *testing.T, got transport.AuthMethod) {
	t.Helper()

	if got != nil {
		t.Errorf("Auth returned %#v, want a nil AuthMethod", got)
	}
}

// writeEd25519Key writes an unencrypted OpenSSH ed25519 private key.
//
// Parameters:
//   - t: Test handle.
//
// Returns:
//   - string: Path to the key file.
func writeEd25519Key(t *testing.T) string {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "id_ed25519")
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(block), 0o600))

	return path
}

// writeKnownHosts writes a single OpenSSH known_hosts line for host.
//
// Parameters:
//   - t: Test handle.
//   - host: Hostname recorded in the file.
//
// Returns:
//   - string: Path to the known_hosts file.
func writeKnownHosts(t *testing.T, host string) string {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	sshPub, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "known_hosts")
	line := host + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(line), 0o600))

	return path
}
