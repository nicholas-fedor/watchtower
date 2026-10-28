package rebuild

import (
	"net/url"
	"strings"

	"github.com/distribution/reference"
	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/internal/container/oci"
	"github.com/nicholas-fedor/watchtower/internal/container/provenance"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// gitShortSHALen is the number of hex characters used in name:git-<shortsha> tags and notifications.
const gitShortSHALen = 12

// withGitMeta adds the repository, ref, selected tag, commit, short commit, and changelog when they are known.
//
// Parameters:
//   - evt: Log event to extend.
//   - c: Container the event describes.
//   - params: Update parameters used to resolve the report fields.
//   - tag: New tag for changelog placeholders.
//   - commit: New commit for changelog placeholders.
//
// Returns:
//   - *zerolog.Event: evt, or nil when evt is nil.
func withGitMeta(
	evt *zerolog.Event,
	c types.Container,
	params types.UpdateParams,
	tag, commit string,
) *zerolog.Event {
	if evt == nil {
		return nil
	}

	meta := provenance.ResolveReportMeta(c, params, provenance.ChangelogVars{
		Tag:    tag,
		Commit: commit,
	}, oci.Annotations{})
	if meta.GitRepo != "" {
		evt = evt.Str("repo", meta.GitRepo)
	}

	if meta.GitRef != "" {
		evt = evt.Str("ref", meta.GitRef)
	}

	if tag != "" {
		evt = evt.Str("tag", tag)
	}

	if commit != "" {
		evt = evt.Str("commit", commit)
		if short := shortGitSHA(commit); short != "" && short != commit {
			evt = evt.Str("short_commit", short)
		}
	}

	if meta.Changelog != "" {
		evt = evt.Str("changelog", meta.Changelog)
	}

	return evt
}

// logFoundGitRevision records that a hosted ref advanced, before a build or checkout.
//
// Parameters:
//   - log: Process logger.
//   - c: Container whose repository is being checked.
//   - params: Update parameters used to resolve the repository and ref.
//   - tag: Selected semver tag, or empty for a branch tip.
//   - commit: Target commit SHA.
//
// Returns:
//   - none.
func logFoundGitRevision(log *zerolog.Logger, c types.Container, params types.UpdateParams, tag, commit string) {
	if log == nil || c == nil {
		return
	}

	assoc, _ := provenance.ResolveAssociation(c, params)

	ref := tag
	if ref == "" {
		ref = assoc.Ref
	}

	identity := gitNoticePath(assoc.Repo)
	if ref != "" && identity != "" {
		identity += "@" + ref
	}

	log.Info().
		Str("container", c.Name()).
		Str("revision", identity).
		Str("short_commit", shortGitSHA(commit)).
		Msg("Found new Git revision")
}

// gitNoticePath returns the host and repository path used in a notification.
//
// Parameters:
//   - repo: Clone URL, including https, ssh, or git@host:path forms.
//
// Returns:
//   - string: Host and path without a scheme or .git suffix.
func gitNoticePath(repo string) string {
	repo = strings.TrimSpace(repo)

	repo = strings.TrimSuffix(repo, ".git")
	if rest, ok := strings.CutPrefix(repo, "git@"); ok {
		host, path, found := strings.Cut(rest, ":")
		if found {
			return host + "/" + strings.TrimPrefix(path, "/")
		}
	}

	parsed, err := url.Parse(repo)
	if err == nil && parsed.Host != "" {
		return parsed.Host + "/" + strings.TrimPrefix(parsed.Path, "/")
	}

	return strings.TrimPrefix(repo, "/")
}

// familiarImage returns the image name a notification should show.
//
// Parameters:
//   - imageName: Docker image reference, possibly with the default registry prefix.
//
// Returns:
//   - string: Familiar name and tag, or imageName when it cannot be parsed.
func familiarImage(imageName string) string {
	ref, err := reference.ParseDockerRef(imageName)
	if err != nil {
		return imageName
	}

	return reference.FamiliarString(ref)
}

// shortGitSHA returns the notification form of a commit SHA.
//
// Parameters:
//   - commit: Full or already-short commit SHA.
//
// Returns:
//   - string: At most gitShortSHALen characters, or commit when it is already short.
func shortGitSHA(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) <= gitShortSHALen {
		return commit
	}

	return commit[:gitShortSHALen]
}
