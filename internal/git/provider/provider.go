package provider

import (
	"net"
	"strings"
)

// Detected REST/changelog provider names. Unmapped hosts use go-git only.
const (
	// GitHub is GitHub.com or GitHub Enterprise Server.
	GitHub = "github"
	// GitLab is GitLab.com or self-hosted GitLab.
	GitLab = "gitlab"
	// Gitea is Gitea (Forgejo uses the same API and changelog URLs).
	Gitea = "gitea"
	// Forgejo is treated as Gitea.
	Forgejo = "forgejo"
)

// canonicalHostKind maps a provider name to github, gitlab, or gitea.
//
// Forgejo shares Gitea's API and release URL shape.
//
// Parameters:
//   - kind: Provider name.
//
// Returns:
//   - string: github, gitlab, gitea, or empty.
func canonicalHostKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case GitHub:
		return GitHub
	case GitLab:
		return GitLab
	case Gitea, Forgejo:
		return Gitea
	default:
		return ""
	}
}

// ResolveHostKind classifies a repository hostname.
//
// github.com, gitlab.com, and codeberg.org are always classified.
// codeberg.org is a public Forgejo host and uses the Gitea/Forgejo API.
// Other hosts use extra mappings when present, typically from tests.
// An empty result means go-git only.
//
// Parameters:
//   - host: Hostname without scheme or port.
//   - extra: Optional hostname-to-provider map (keys lowercased).
//
// Returns:
//   - string: github, gitlab, gitea, or empty.
func ResolveHostKind(host string, extra map[string]string) string {
	host = strings.ToLower(strings.TrimSpace(host))

	host = strings.Trim(host, "[]")

	hostname, _, err := net.SplitHostPort(host)
	if err == nil {
		host = hostname
	}

	host = strings.TrimSuffix(host, ".")

	switch host {
	case "github.com":
		return GitHub
	case "gitlab.com":
		return GitLab
	case "codeberg.org":
		return Gitea
	}

	if extra == nil {
		return ""
	}

	return canonicalHostKind(extra[host])
}
