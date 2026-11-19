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
//   - host: Hostname without scheme, optionally with a port. Spellings that
//     [Hostname] treats as the same host classify the same way.
//
// Returns:
//   - string: github, gitlab, gitea, or empty.
func ResolveHostKind(host string) string {
	switch Hostname(host) {
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

// Hostname returns host in the form Git hosts are compared in.
//
// Two spellings name the same host when their forms are equal, so
// GitHub.com.:443 and github.com compare equal. The form ignores these parts
// of a spelling:
//   - surrounding spaces
//   - a port
//   - IPv6 brackets
//   - letter case
//   - the trailing dot of a fully qualified name
//
// An IPv6 literal carries a port only inside brackets, so a bare literal such
// as 2001:db8::1 is kept whole.
//
// Parameters:
//   - host: Hostname, optionally with a port.
//
// Returns:
//   - string: Lowercase hostname without a port, brackets or a trailing dot.
func Hostname(host string) string {
	host = strings.TrimSpace(host)

	hostname, _, err := net.SplitHostPort(host)
	if err == nil {
		host = hostname
	}

	return strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
}
