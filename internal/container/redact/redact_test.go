package redact

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_redactedRemote(t *testing.T) {
	t.Parallel()

	t.Run("replaces a password and leaves other URLs unchanged", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "https://xxxxx:xxxxx@github.com/org/app.git#abc", Remote("https://user:secret@github.com/org/app.git#abc"))
		assert.Equal(t, "https://github.com/org/app.git", Remote("https://github.com/org/app.git"))
		assert.Equal(t, "://bad", Remote("://bad"))
	})

	t.Run("masks the userinfo of a remote that does not parse", func(t *testing.T) {
		t.Parallel()

		// The control character makes url.Parse reject the remote.
		assert.Equal(t,
			"https://xxxxx:xxxxx@github.com/org/app.git\x7f",
			Remote("https://user:secret@github.com/org/app.git\x7f"),
		)
	})
}

// TestRedactGitURL verifies that a Git URL user that looks like a token is
// dropped from SCP-style and ssh:// URLs, while ordinary SSH users are kept.
func TestRedactGitURL(t *testing.T) {
	t.Parallel()

	assert.Empty(t, GitURL("token@github.com:org/app.git"))
	assert.Empty(t, GitURL("ghp_abcdefghijklmnopqrstuvwxyz@github.com:org/app.git"))
	assert.Equal(t, "git@github.com:org/app.git", GitURL("git@github.com:org/app.git"))
	assert.Equal(t, "deploy@git.example.com:org/app.git", GitURL("deploy@git.example.com:org/app.git"))

	assert.Equal(t, "ssh://github.com/org/app.git", GitURL("ssh://ghp_abcdefghijklmnopqrstuvwxyz@github.com/org/app.git"))
	assert.Equal(t, "ssh://git@github.com/org/app.git", GitURL("ssh://git@github.com/org/app.git"))
	assert.Equal(t, "ssh://git@github.com/org/app.git", GitURL("ssh://git:secret@github.com/org/app.git"))
}

// TestGitURL_SensitiveQueryKeys verifies that query parameters named for
// credentials are removed from URL, SCP-style, and unqualified repository
// values. That includes camelCase names and the separated forms. Ordinary
// parameters are kept, as are names that contain a credential word only inside
// a longer word.
func TestGitURL_SensitiveQueryKeys(t *testing.T) {
	t.Parallel()

	sensitive := []string{
		"access_token",
		"accessToken",
		"apiToken",
		"APIToken",
		"refreshToken",
		"clientSecret",
		"client-secret",
		"PassWord",
		"pwd",
		"passphrase",
		"keyPassphrase",
		"sshPassPhrase",
		"dbPassWord",
		"pass_word",
		"authToken",
		"sig",
	}
	benign := []string{"author", "authorName", "design", "tokenizer", "passThrough", "format", "lang", "version2"}

	forms := []struct {
		name   string
		repo   string
		prefix string
	}{
		{name: "URL", repo: "https://github.com/org/app.git", prefix: "https://github.com/org/app.git"},
		{name: "SCP-style", repo: "git@github.com:org/app.git", prefix: "git@github.com:org/app.git"},
		{name: "unqualified path", repo: "repos/app", prefix: "repos/app"},
	}

	for _, form := range forms {
		t.Run(form.name, func(t *testing.T) {
			t.Parallel()

			for _, key := range sensitive {
				assert.Equal(t, form.prefix+"?ref=main#src", GitURL(form.repo+"?"+key+"=secret&ref=main#src"), key)
			}

			for _, key := range benign {
				assert.Equal(t, form.prefix+"?"+key+"=1", GitURL(form.repo+"?"+key+"=1"), key)
			}
		})
	}
}

// TestQueryKeyWords verifies where query parameter names split into words. The
// cases cover separators, camelCase boundaries, and runs of capitals.
func TestQueryKeyWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want []string
	}{
		{key: "", want: []string{}},
		{key: "token", want: []string{"token"}},
		{key: "access_token", want: []string{"access", "token"}},
		{key: "client-secret.v2", want: []string{"client", "secret", "v2"}},
		{key: "accessToken", want: []string{"access", "token"}},
		{key: "APIToken", want: []string{"api", "token"}},
		{key: "JWT", want: []string{"jwt"}},
		{key: "key2Token", want: []string{"key2", "token"}},
		{key: "__token__", want: []string{"token"}},
		{key: "ÜberToken", want: []string{"über", "token"}},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, queryKeyWords(tt.key))
		})
	}
}

// TestGitURL_Forms verifies which values GitURL keeps as they are and which it
// rewrites or rejects.
func TestGitURL_Forms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "blank", raw: "   ", want: ""},
		{name: "relative path", raw: "./repos/app", want: "./repos/app"},
		{name: "home path", raw: "~/repos/app", want: "~/repos/app"},
		{name: "Windows path", raw: `C:\repos\app`, want: `C:\repos\app`},
		{name: "unparseable URL", raw: "https://github.com/%zz", want: ""},
		{name: "URL without a host", raw: "https:///org/app.git", want: ""},
		{name: "file URL", raw: "file:///srv/repos/app.git", want: "file:///srv/repos/app.git"},
		{name: "file URL user", raw: "file://user@/srv/repos/app.git", want: "file:///srv/repos/app.git"},
		{name: "https user without a password", raw: "https://deploy@github.com/org/app.git", want: "https://github.com/org/app.git"},
		{name: "empty ssh user", raw: "ssh://@github.com/org/app.git", want: "ssh://github.com/org/app.git"},
		{name: "long hex ssh user", raw: "ssh://0123456789abcdef0123@github.com/org/app.git", want: "ssh://github.com/org/app.git"},
		{name: "long ssh account name", raw: "ssh://deploy-user-for-ci-builds@github.com/org/app.git", want: "ssh://deploy-user-for-ci-builds@github.com/org/app.git"},
		{name: "query key with an invalid escape", raw: "https://github.com/org/app.git?%zz=1&ref=main", want: "https://github.com/org/app.git?ref=main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, GitURL(tt.raw))
		})
	}
}

// TestGitURL_MalformedSCPRemotes verifies that SCP-style remotes GitURL cannot
// split into a user, a host, and a path are rejected rather than stored.
func TestGitURL_MalformedSCPRemotes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "empty user", raw: "@github.com:org/app.git"},
		{name: "user with a password", raw: "git:secret@github.com:org/app.git"},
		{name: "empty host", raw: "git@:org/app.git"},
		{name: "empty path", raw: "git@github.com:"},
		{name: "host with a path", raw: "git@github.com/org:app.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, GitURL(tt.raw))
		})
	}
}

// TestMetadataURL verifies that URL-like report metadata is redacted like a
// Git URL, while plain annotation values pass through trimmed.
func TestMetadataURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "blank", raw: "  ", want: ""},
		{name: "version", raw: " v1.2.3 ", want: "v1.2.3"},
		{name: "license", raw: "Apache-2.0", want: "Apache-2.0"},
		{name: "path without userinfo", raw: "team/app", want: "team/app"},
		{name: "URL with credentials", raw: "https://user:secret@example.com/docs?token=x&lang=en", want: "https://example.com/docs?lang=en"},
		{name: "SCP-style remote", raw: "git@github.com:org/app.git", want: "git@github.com:org/app.git"},
		{name: "path with a query", raw: "docs?token=x&lang=en", want: "docs?lang=en"},
		{name: "userinfo without a scheme", raw: "user:secret@example.com/app", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, MetadataURL(tt.raw))
		})
	}
}

// TestUserinfo verifies that error text loses the credentials of the remote it
// quotes, in plain or query-escaped form. Any other URL userinfo in the text is
// masked too.
func TestUserinfo(t *testing.T) {
	t.Parallel()

	const remote = "https://user:secret@github.com/org/app.git"

	redacted := "https://xxxxx:xxxxx@github.com/org/app.git"

	tests := []struct {
		name   string
		msg    string
		remote string
		want   string
	}{
		{
			name:   "quoted remote",
			msg:    "clone " + remote + " failed",
			remote: remote,
			want:   "clone " + redacted + " failed",
		},
		{
			name:   "query-escaped remote",
			msg:    "POST /build?remote=" + url.QueryEscape(remote) + ": denied",
			remote: remote,
			want:   "POST /build?remote=" + url.QueryEscape(redacted) + ": denied",
		},
		{
			name:   "remote with a user only",
			msg:    "clone https://token@github.com/org/app.git failed",
			remote: "https://token@github.com/org/app.git",
			want:   "clone https://xxxxx:xxxxx@github.com/org/app.git failed",
		},
		{
			name: "other userinfo",
			msg:  "fetch ssh://deploy:pw@git.example.com/org/app.git failed",
			want: "fetch ssh://xxxxx:xxxxx@git.example.com/org/app.git failed",
		},
		{
			name: "password containing @",
			msg:  "fetch https://user:p@ss@git.example.com/org/app.git failed",
			want: "fetch https://xxxxx:xxxxx@git.example.com/org/app.git failed",
		},
		{
			name: "no credentials",
			msg:  "build failed: no such file",
			want: "build failed: no such file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Userinfo(tt.msg, tt.remote))
		})
	}
}

// TestIsHexString verifies which values count as hexadecimal, including the
// empty value that callers do not reach through GitURL.
func TestIsHexString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  bool
	}{
		{value: "", want: false},
		{value: "0123456789abcdefABCDEF", want: true},
		{value: "deadbeefg", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, isHexString(tt.value))
		})
	}
}

// TestServiceURL verifies that a notification service URL keeps only its
// scheme wherever its credentials sit. A value without a usable scheme is
// replaced whole.
func TestServiceURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: ""},
		{name: "credentials in the userinfo", raw: "smtp://user:secret@mail.example.com:587/?from=a@example.com", want: "smtp://[redacted]"},
		{name: "token in the path", raw: "https://hooks.slack.com/services/T000/B000/XXXX", want: "https://[redacted]"},
		{name: "token in the query", raw: "gotify://gotify.example.com/?token=secret", want: "gotify://[redacted]"},
		{name: "token as the host", raw: "telegram://123456:secret@telegram?chats=1", want: "telegram://[redacted]"},
		{name: "no scheme", raw: "hooks.slack.com/services/T000/B000/XXXX", want: "[redacted]"},
		{name: "does not parse", raw: "slack://token\x7f", want: "[redacted]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, ServiceURL(tt.raw))
		})
	}
}

// TestServiceURLs verifies that each URL is redacted in place, so the summaries
// line up with the configured URLs. No URLs give nil.
func TestServiceURLs(t *testing.T) {
	t.Parallel()

	assert.Nil(t, ServiceURLs(nil))
	assert.Nil(t, ServiceURLs([]string{}))
	assert.Equal(t,
		[]string{"slack://[redacted]", "", "logger://[redacted]"},
		ServiceURLs([]string{"slack://token@channel", "", "logger://"}),
	)
}
