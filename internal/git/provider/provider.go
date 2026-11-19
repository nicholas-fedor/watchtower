package provider

import (
	"net"
	"strings"
)

// Detected REST/changelog provider names. Unclassified hosts use go-git only.
const (
	// GitHub is GitHub.com or GitHub Enterprise Server.
	GitHub = "github"
	// GitLab is GitLab.com or self-hosted GitLab.
	GitLab = "gitlab"
	// Gitea is Gitea (Forgejo uses the same API and changelog URLs).
	Gitea = "gitea"
	// Forgejo names a Forgejo server in a git-host label path. Forgejo
	// serves the Gitea API, so it resolves to Gitea.
	Forgejo = "forgejo"
)

// ResolveHostKind classifies a repository hostname.
//
// github.com, gitlab.com, and codeberg.org are classified. codeberg.org is a
// public Forgejo host and uses the Gitea/Forgejo API. Every other host returns
// empty, which means go-git only.
//
// Parameters:
//   - host: Hostname without scheme. A port, IPv6 brackets, a trailing dot,
//     and uppercase letters are ignored.
//
// Returns:
//   - string: github, gitlab, gitea, or empty.
func ResolveHostKind(host string) string {
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
	default:
		return ""
	}
}
