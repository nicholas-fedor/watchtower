package provider

import (
	"strings"
	"testing"
)

// FuzzCanonicalHostKind verifies provider names never panic and only
// collapse to github, gitlab, gitea, or empty.
func FuzzCanonicalHostKind(f *testing.F) {
	f.Add("github")
	f.Add("GitHub")
	f.Add("gitlab")
	f.Add("gitea")
	f.Add("forgejo")
	f.Add("bitbucket")
	f.Add("")
	f.Add("  GITEA  ")

	f.Fuzz(func(t *testing.T, kind string) {
		got := canonicalHostKind(kind)
		switch got {
		case "", GitHub, GitLab, Gitea:
		default:
			t.Fatalf("unexpected kind %q", got)
		}
	})
}

// FuzzResolveHostKind verifies hostname classification never panics.
func FuzzResolveHostKind(f *testing.F) {
	f.Add("github.com", "")
	f.Add("gitlab.com", "")
	f.Add("codeberg.org", "")
	f.Add("git.example.com", "")
	f.Add("git.example.com", "gitea")
	f.Add("GITHUB.COM", "")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, host, extraKind string) {
		var extra map[string]string
		if extraKind != "" {
			extra = map[string]string{strings.ToLower(strings.TrimSpace(host)): extraKind}
		}

		got := ResolveHostKind(host, extra)
		switch got {
		case "", GitHub, GitLab, Gitea:
		default:
			t.Fatalf("unexpected kind %q", got)
		}
	})
}
