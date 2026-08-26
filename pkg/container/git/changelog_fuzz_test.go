package git

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// FuzzParseRepo verifies clone URLs from labels never panic and that a
// successful split has host, owner, and name.
func FuzzParseRepo(f *testing.F) {
	f.Add("https://github.com/org/app.git")
	f.Add("git@github.com:org/app.git")
	f.Add("ssh://git@gitlab.com/group/app.git")
	f.Add("github.com/org/app")
	f.Add("https://gitlab.com/a/b/c.git")
	f.Add("git@:0/0")
	f.Add("git@github.com")
	f.Add("https://github.com")
	f.Add("https://")
	f.Add("")
	f.Add("  ")

	f.Fuzz(func(t *testing.T, raw string) {
		host, owner, name, ok := parseRepo(raw)
		if !ok {
			assert.Empty(t, host)
			assert.Empty(t, owner)
			assert.Empty(t, name)

			return
		}

		assert.NotEmpty(t, host)
		assert.NotEmpty(t, owner)
		assert.NotEmpty(t, name)
		assert.False(t, strings.HasSuffix(name, ".git"))
	})
}

// FuzzApplyVars verifies changelog templates never panic.
func FuzzApplyVars(f *testing.F) {
	f.Add("https://example.com/{tag}", "v1.2.3", "abc")
	f.Add("https://example.com/{major}.{minor}.{patch}", "1.2.3", "")
	f.Add("https://example.com/{commit}", "nightly", "deadbeef")
	f.Add("{tag}{commit}{major}", "", "")
	f.Add("", "v1.0.0", "abc")

	f.Fuzz(func(_ *testing.T, template, tag, commit string) {
		_ = applyVars(template, ChangelogVars{Tag: tag, Commit: commit})
	})
}

// FuzzDerivedReleasesURL verifies release URL derivation never panics.
func FuzzDerivedReleasesURL(f *testing.F) {
	f.Add("https://github.com/org/app.git", "")
	f.Add("https://gitlab.com/org/app.git", "")
	f.Add("https://codeberg.org/org/app.git", "")
	f.Add("https://git.example.com/org/app.git", "git.example.com=gitea")
	f.Add("https://unknown.example/org/app.git", "")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, repo, extra string) {
		hosts := map[string]string{}

		if extra != "" {
			name, kind, ok := strings.Cut(extra, "=")
			if ok {
				hosts[name] = kind
			}
		}

		got := derivedReleasesURL(repo, "")
		if got == "" {
			return
		}

		assert.True(t, strings.HasPrefix(got, "http://") || strings.HasPrefix(got, "https://"), got)
		assert.NotContains(t, got, "..")
	})
}
