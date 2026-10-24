package redact

import (
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
		"authToken",
		"sig",
	}
	benign := []string{"author", "authorName", "design", "tokenizer", "format", "lang", "version2"}

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
