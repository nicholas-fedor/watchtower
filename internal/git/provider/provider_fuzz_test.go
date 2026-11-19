package provider

import (
	"strings"
	"testing"
)

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

// FuzzHostname verifies these properties of Hostname:
//   - It never panics.
//   - Its result has no ASCII uppercase letter.
//   - A host and its spelling with ASCII letters uppercased get the same form.
//
// The seed corpus holds these hosts:
//   - a plain host
//   - an uppercase fully qualified name with a port
//   - a bracketed IPv6 literal
//   - a host with surrounding spaces
//   - the empty string
func FuzzHostname(f *testing.F) {
	f.Add("git.example.com")
	f.Add("GitHub.com.:443")
	f.Add("[2001:db8::1]")
	f.Add(" git.example.com ")
	f.Add("")

	f.Fuzz(func(t *testing.T, host string) {
		got := Hostname(host)
		if strings.ContainsFunc(got, isASCIIUpper) {
			t.Fatalf("Hostname(%q) = %q has an uppercase letter", host, got)
		}

		upper := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' {
				return r - 'a' + 'A'
			}

			return r
		}, host)
		if Hostname(upper) != got {
			t.Fatalf("Hostname(%q) differs from Hostname(%q)", upper, host)
		}
	})
}

// isASCIIUpper reports whether r is an ASCII uppercase letter.
//
// Parameters:
//   - r: Rune to check.
//
// Returns:
//   - bool: True for A through Z.
func isASCIIUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}
