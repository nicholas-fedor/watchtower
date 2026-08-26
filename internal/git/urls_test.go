package git

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyAuth(t *testing.T) {
	t.Parallel()

	t.Run("nil request", func(t *testing.T) {
		t.Parallel()

		(&Client{opts: Options{Token: "tok"}}).applyAuth(nil)
	})

	t.Run("token is bearer", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://git.example.com", nil)
		require.NoError(t, err)

		(&Client{opts: Options{Token: "tok", Username: "user", Password: "pw"}}).applyAuth(req)
		assert.Equal(t, "Bearer tok", req.Header.Get("Authorization"))
	})

	t.Run("basic auth", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://git.example.com", nil)
		require.NoError(t, err)

		(&Client{opts: Options{Username: "octocat", Password: "pw"}}).applyAuth(req)
		user, pass, ok := req.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "octocat", user)
		assert.Equal(t, "pw", pass)
	})

	t.Run("username only", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://git.example.com", nil)
		require.NoError(t, err)

		(&Client{opts: Options{Username: "octocat"}}).applyAuth(req)
		user, pass, ok := req.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "octocat", user)
		assert.Empty(t, pass)
	})

	t.Run("no credentials", func(t *testing.T) {
		t.Parallel()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://git.example.com", nil)
		require.NoError(t, err)

		(&Client{}).applyAuth(req)
		assert.Empty(t, req.Header.Get("Authorization"))
	})
}

func TestOriginOf(t *testing.T) {
	t.Parallel()

	t.Run("nil client defaults to https host", func(t *testing.T) {
		t.Parallel()

		var client *Client

		got := client.originOf("Git.Example.COM")
		assert.Equal(t, "https", got.Scheme)
		assert.Equal(t, "git.example.com", got.Host)
	})

	t.Run("stored origin", func(t *testing.T) {
		t.Parallel()

		stored := url.URL{Scheme: "https", Host: "git.example.com:8443", Path: "/gitlab"}
		client := &Client{origins: map[string]url.URL{"git.example.com": stored}}
		assert.Equal(t, stored, client.originOf("[Git.Example.COM]"))
	})

	t.Run("ipv6 default", func(t *testing.T) {
		t.Parallel()

		got := (&Client{}).originOf("2001:db8::1")
		assert.Equal(t, "https", got.Scheme)
		assert.Equal(t, "[2001:db8::1]", got.Host)
	})
}

func TestHTTPHost(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "git.example.com", httpHost("git.example.com"))
	assert.Equal(t, "[2001:db8::1]", httpHost("2001:db8::1"))
}

func TestGithubAPI(t *testing.T) {
	t.Parallel()

	t.Run("github.com uses api.github.com", func(t *testing.T) {
		t.Parallel()

		got := (&Client{}).githubAPI("GitHub.com", "repos", "org", "app")
		assert.Equal(t, "https://api.github.com/repos/org/app", got.String())
	})

	t.Run("enterprise uses origin", func(t *testing.T) {
		t.Parallel()

		client := &Client{origins: map[string]url.URL{
			"github.company.com": {Scheme: "https", Host: "github.company.com", Path: "/ghe"},
		}}
		got := client.githubAPI("github.company.com", "repos", "org", "app")
		assert.Equal(t, "https://github.company.com/ghe/api/v3/repos/org/app", got.String())
	})

	t.Run("enterprise default origin", func(t *testing.T) {
		t.Parallel()

		got := (&Client{}).githubAPI("github.company.com", "user")
		assert.Equal(t, "https://github.company.com/api/v3/user", got.String())
	})
}

func TestGitlabAPI(t *testing.T) {
	t.Parallel()

	client := &Client{origins: map[string]url.URL{
		"gitlab.internal": {Scheme: "https", Host: "gitlab.internal:8443", Path: "/gitlab"},
	}}
	assert.Equal(t, "https://gitlab.internal:8443/gitlab/api/v4/projects",
		client.gitlabAPI("gitlab.internal", "projects").String())
	assert.Equal(t, "https://gitlab.com/api/v4/version",
		(&Client{}).gitlabAPI("gitlab.com", "version").String())
}

func TestGiteaAPI(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://git.example.com/api/v1/version",
		(&Client{}).giteaAPI("git.example.com", "version").String())
	assert.Equal(t, "https://codeberg.org/api/v1/repos/org/app",
		(&Client{}).giteaAPI("codeberg.org", "repos", "org", "app").String())
}

func TestRejectCrossHostRedirect(t *testing.T) {
	t.Parallel()

	same, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://git.example.com/b", nil)
	require.NoError(t, err)
	prev, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://git.example.com/a", nil)
	require.NoError(t, err)
	require.NoError(t, rejectCrossHostRedirect(same, []*http.Request{prev}))

	other, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://evil.example/b", nil)
	require.NoError(t, err)
	require.ErrorIs(t, rejectCrossHostRedirect(other, []*http.Request{prev}), errCrossHostRedirect)

	downgrade, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://git.example.com/b", nil)
	require.NoError(t, err)
	require.ErrorIs(t, rejectCrossHostRedirect(downgrade, []*http.Request{prev}), errCrossHostRedirect)
}

func TestNewGitHTTPClient(t *testing.T) {
	t.Parallel()

	t.Run("default transport", func(t *testing.T) {
		t.Parallel()

		got := newGitHTTPClient(5*time.Second, false, nil)
		assert.Equal(t, 5*time.Second, got.Timeout)
		assert.Nil(t, got.Transport)
	})

	t.Run("insecure skip verify", func(t *testing.T) {
		t.Parallel()

		got := newGitHTTPClient(time.Second, true, nil)
		tr, ok := got.Transport.(*http.Transport)
		require.True(t, ok)
		require.NotNil(t, tr.TLSClientConfig)
		assert.True(t, tr.TLSClientConfig.InsecureSkipVerify)
		assert.Equal(t, uint16(tls.VersionTLS12), tr.TLSClientConfig.MinVersion)
	})

	t.Run("ca bundle", func(t *testing.T) {
		t.Parallel()

		got := newGitHTTPClient(time.Second, false, []byte("not-a-real-pem"))
		tr, ok := got.Transport.(*http.Transport)
		require.True(t, ok)
		require.NotNil(t, tr.TLSClientConfig)
		assert.Nil(t, tr.TLSClientConfig.RootCAs)
		assert.False(t, tr.TLSClientConfig.InsecureSkipVerify)
	})
}

func TestToHostname(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "git.example.com", toHostname("Git.Example.COM"))
	assert.Equal(t, "2001:db8::1", toHostname("[2001:db8::1]"))
	assert.Equal(t, "git.example.com", toHostname("[git.example.com]"))
	assert.Empty(t, toHostname(""))
}

func TestJoinEscaped(t *testing.T) {
	t.Parallel()

	t.Run("nil base", func(t *testing.T) {
		t.Parallel()

		got := joinEscaped(nil, "projects", "group/app")
		assert.Empty(t, got.String())
	})

	t.Run("escapes slashes in segments", func(t *testing.T) {
		t.Parallel()

		base := &url.URL{Scheme: "https", Host: "gitlab.com", Path: "/api/v4"}
		got := joinEscaped(base, "projects", "group/app", "repository", "commits", "main")
		assert.Equal(t, "https://gitlab.com/api/v4/projects/group%2Fapp/repository/commits/main", got.String())
	})

	t.Run("trims trailing slash on base", func(t *testing.T) {
		t.Parallel()

		base := &url.URL{Scheme: "https", Host: "gitlab.com", Path: "/api/v4/"}
		got := joinEscaped(base, "version")
		assert.Equal(t, "https://gitlab.com/api/v4/version", got.String())
	})

	t.Run("falls back to join path when parse fails", func(t *testing.T) {
		t.Parallel()

		base := &url.URL{Scheme: ":", Host: "x.com"}
		got := joinEscaped(base, "projects", "group/app")
		assert.NotNil(t, got)
		assert.Contains(t, got.Path, "projects")
	})
}
