package git

import (
	"net/url"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	semverPartCount = 3
	patchIndex      = 2
)

// ChangelogVars substitutes placeholders in an explicit changelog template.
type ChangelogVars struct {
	Tag    string
	Commit string
}

// Changelog returns an explicit template, a derived releases URL, or empty.
//
// It does not read OCI annotations. Callers may fall back to OCI URL or docs.
//
// Parameters:
//   - c: Container that may carry a changelog or git-host label.
//   - repo: Associated or inferred Git clone URL.
//   - vars: Placeholder values for an explicit template.
//
// Returns:
//   - string: Changelog URL, or empty when none can be derived.
func Changelog(c types.Container, repo string, vars ChangelogVars) string {
	if template := label(c, ChangelogLabel); template != "" {
		return applyVars(template, vars)
	}

	apiOrigin := ""
	if c != nil {
		apiOrigin = label(c, HostLabel)
	}

	return derivedReleasesURL(repo, apiOrigin)
}

// applyVars substitutes {tag}, {commit}, and semver placeholders in template.
//
// Parameters:
//   - template: URL template from the changelog label.
//   - vars: Values to substitute. Unknown parts leave the placeholder intact.
//
// Returns:
//   - string: Template after substitution.
func applyVars(template string, vars ChangelogVars) string {
	major, minor, patch := splitSemverParts(vars.Tag)

	replacer := strings.NewReplacer(
		"{major}", orUnchanged(major, "{major}"),
		"{minor}", orUnchanged(minor, "{minor}"),
		"{patch}", orUnchanged(patch, "{patch}"),
		"{tag}", orUnchanged(vars.Tag, "{tag}"),
		"{commit}", orUnchanged(vars.Commit, "{commit}"),
	)

	return replacer.Replace(template)
}

// orUnchanged returns value, or placeholder when value is empty.
//
// Parameters:
//   - value: Substitution value.
//   - placeholder: Token to keep when value is empty.
//
// Returns:
//   - string: Value or the original placeholder.
func orUnchanged(value, placeholder string) string {
	if value == "" {
		return placeholder
	}

	return value
}

// splitSemverParts returns major, minor, and patch from a tag.
//
// Parameters:
//   - tag: Version tag, with or without a v prefix.
//
// Returns:
//   - string: Major component, or empty.
//   - string: Minor component, or empty.
//   - string: Patch component without prerelease suffix, or empty.
func splitSemverParts(tag string) (string, string, string) {
	canonical := canonicalizeSemver(tag)
	if !semver.IsValid(canonical) {
		return "", "", ""
	}

	trimmed := strings.TrimPrefix(semver.Canonical(canonical), "v")
	parts := strings.SplitN(trimmed, ".", semverPartCount)

	major, minor, patch := "", "", ""
	if len(parts) > 0 {
		major = parts[0]
	}

	if len(parts) > 1 {
		minor = parts[1]
	}

	if len(parts) > patchIndex {
		patch, _, _ = strings.Cut(parts[patchIndex], "-")
	}

	return major, minor, patch
}

// canonicalizeSemver returns a semver string golang.org/x/mod/semver accepts.
//
// Parameters:
//   - tag: Raw tag.
//
// Returns:
//   - string: Tag, optionally prefixed with v, or the original string if invalid.
func canonicalizeSemver(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}

	if semver.IsValid(tag) {
		return tag
	}

	if semver.IsValid("v" + tag) {
		return "v" + tag
	}

	return tag
}

// derivedReleasesURL builds a product-specific releases URL for a clone URL.
//
// Parameters:
//   - repo: Git clone URL.
//   - apiOrigin: Optional HTTP API base URL from the git-host label.
//
// Returns:
//   - string: Releases URL, or empty for unknown hosts.
func derivedReleasesURL(repo, apiOrigin string) string {
	host, owner, name, ok := parseRepo(repo)
	if !ok {
		return ""
	}

	cloneKind := types.ResolveGitHostKind(host, nil)
	kind := cloneKind
	base := "https://" + host

	if apiOrigin != "" {
		origin, err := ParseAPIOrigin(apiOrigin)
		if err != nil || !sameChangelogHost(host, origin) {
			return ""
		}

		if cloneKind != "" {
			base = strings.TrimRight(origin.Scheme+"://"+origin.Host, "/")
		} else {
			kind = changelogProvider(origin.Path, "")
			if kind == "" {
				return ""
			}

			base = strings.TrimRight(origin.Scheme+"://"+origin.Host+origin.Path, "/")
		}
	}

	if kind == "" {
		return ""
	}

	path := owner + "/" + name

	switch kind {
	case types.GitHostGitHub:
		return base + "/" + path + "/releases"
	case types.GitHostGitLab:
		return base + "/" + path + "/-/releases"
	case types.GitHostGitea:
		return base + "/" + path + "/releases"
	default:
		return ""
	}
}

// sameChangelogHost reports whether a clone host and API origin share a hostname.
//
// Parameters:
//   - cloneHost: Hostname with an optional port from the clone URL.
//   - origin: Parsed API origin.
//
// Returns:
//   - bool: True when the hostnames match, ignoring ports.
func sameChangelogHost(cloneHost string, origin url.URL) bool {
	parsed, err := url.Parse("//" + cloneHost)
	if err != nil {
		return false
	}

	return strings.EqualFold(parsed.Hostname(), origin.Hostname())
}

// changelogProvider identifies a provider from a documented API path prefix.
//
// Parameters:
//   - pathPrefix: API path prefix from git-host.
//   - known: Provider already identified from the clone hostname.
//
// Returns:
//   - string: GitHub, GitLab, Gitea, or empty.
func changelogProvider(pathPrefix, known string) string {
	if known != "" {
		return known
	}

	prefix := strings.ToLower(strings.Trim(pathPrefix, "/"))
	if prefix == "" || strings.Contains(prefix, "/") {
		return ""
	}

	switch prefix {
	case types.GitHostGitHub:
		return types.GitHostGitHub
	case types.GitHostGitLab:
		return types.GitHostGitLab
	case types.GitHostGitea, types.GitHostForgejo, "codeberg":
		return types.GitHostGitea
	default:
		return ""
	}
}

// parseRepo extracts host, owner, and repo name from a clone URL.
//
// Parameters:
//   - raw: HTTPS, SSH, or scp-like Git URL.
//
// Returns:
//   - string: Lowercase hostname.
//   - string: Owner or group path.
//   - string: Repository name without .git.
//   - bool: True when the URL has a usable owner and name.
func parseRepo(raw string) (string, string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", false
	}

	raw = strings.TrimSuffix(raw, ".git")

	// SCP-like remotes: git@host:owner/name.git
	if strings.HasPrefix(raw, "git@") {
		_, rest, found := strings.Cut(raw, "@")
		if !found {
			return "", "", "", false
		}

		hostPart, pathPart, found := strings.Cut(rest, ":")
		if !found {
			return "", "", "", false
		}

		return splitOwnerRepo(strings.ToLower(hostPart), pathPart)
	}

	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", "", "", false
	}

	return splitOwnerRepo(strings.ToLower(parsed.Host), strings.TrimPrefix(parsed.Path, "/"))
}

// splitOwnerRepo splits a Git path into owner and repository name.
//
// Nested groups keep slashes in owner (group/sub).
//
// Parameters:
//   - host: Hostname.
//   - path: Path after the host, with or without a leading slash.
//
// Returns:
//   - string: Host.
//   - string: Owner path.
//   - string: Repository name.
//   - bool: True when both owner and name are non-empty.
func splitOwnerRepo(host, path string) (string, string, string, bool) {
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")

	slash := strings.LastIndex(path, "/")
	if slash <= 0 || slash == len(path)-1 {
		return "", "", "", false
	}

	owner := path[:slash]
	name := path[slash+1:]

	if host == "" || owner == "" || name == "" {
		return "", "", "", false
	}

	return host, owner, name, true
}
