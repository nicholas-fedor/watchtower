package provider

import "testing"

// FuzzResolveHostKind verifies hostname classification never panics and
// returns only github, gitlab, gitea, or empty.
//
// The seed corpus holds each built-in host, an unclassified host, an
// uppercase spelling, and the empty string.
func FuzzResolveHostKind(f *testing.F) {
	f.Add("github.com")
	f.Add("gitlab.com")
	f.Add("codeberg.org")
	f.Add("git.example.com")
	f.Add("GITHUB.COM")
	f.Add("")

	f.Fuzz(func(t *testing.T, host string) {
		got := ResolveHostKind(host)
		switch got {
		case "", GitHub, GitLab, Gitea:
		default:
			t.Fatalf("unexpected kind %q", got)
		}
	})
}
