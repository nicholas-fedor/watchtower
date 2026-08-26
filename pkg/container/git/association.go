package git

import (
	"cmp"
	"strings"

	"github.com/rs/zerolog"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

const (
	// DefaultDockerfile is used when no Dockerfile path is configured.
	DefaultDockerfile = "Dockerfile"
	// DefaultContext is the repository root of a Git URL context.
	DefaultContext = "."
)

// Association is a Git monitor association from labels or image mapping.
type Association struct {
	Repo       string
	Ref        string
	Policy     string
	Host       string
	Dockerfile string
	Context    string
}

// ResolveAssociation returns the monitor association for a container.
//
// Highest wins: Watchtower labels, then an exact ImageName() mapping.
// OCI annotations never associate a container.
//
// Parameters:
//   - c: Container to inspect.
//   - params: Update parameters with image mappings and defaults.
//
// Returns:
//   - Association: Repo, ref, policy, and user-specified build paths when associated.
//     Dockerfile and Context are empty when unset. Callers default to Dockerfile
//     and the repository root.
//   - bool: True when a git-repo label or image mapping is present.
func ResolveAssociation(c types.Container, params types.UpdateParams) (Association, bool) {
	if c == nil {
		return Association{}, false
	}

	// Container labels win over --git-image mappings.
	if repo := label(c, RepoLabel); repo != "" {
		return Association{
			Repo:       repo,
			Ref:        cmp.Or(refLabel(c), defaultRef(params)),
			Policy:     cmp.Or(policyLabel(c), defaultPolicy(params)),
			Host:       label(c, HostLabel),
			Dockerfile: cmp.Or(label(c, DockerfileLabel), params.GitDockerfile),
			Context:    cmp.Or(label(c, ContextLabel), params.GitContext),
		}, true
	}

	mapping := imageMapping(c.ImageName(), params.GitImages)
	if mapping.Repo == "" {
		return Association{}, false
	}

	return Association{
		Repo:       mapping.Repo,
		Ref:        cmp.Or(mapping.Ref, defaultRef(params)),
		Policy:     cmp.Or(mapping.Policy, defaultPolicy(params)),
		Host:       label(c, HostLabel),
		Dockerfile: cmp.Or(label(c, DockerfileLabel), mapping.Dockerfile, params.GitDockerfile),
		Context:    cmp.Or(label(c, ContextLabel), mapping.Context, params.GitContext),
	}, true
}

// ShouldMonitor reports whether Git monitoring should run for the container.
//
// Watchtower itself is excluded so registry self-update remains the default path.
// The container must be associated via a git-repo label or git-image mapping.
// git-watch=true without association stays on the registry path.
//
// Parameters:
//   - log: Optional logger for the unassociated git-watch=true debug line.
//   - c: Container to inspect.
//   - params: Update parameters including the process-wide monitor default.
//
// Returns:
//   - bool: True when Git staleness checks and rebuilds apply.
func ShouldMonitor(log *zerolog.Logger, c types.Container, params types.UpdateParams) bool {
	if c == nil || c.IsWatchtower() {
		return false
	}

	_, associated := ResolveAssociation(c, params)
	watch := isWatch(c, params)

	// A watch override without association is a misconfig. Stay on the registry path.
	if watch && !associated {
		if log != nil {
			log.Debug().
				Str("container", c.Name()).
				Str("image", c.ImageName()).
				Msg("git-watch is set but container is not associated via git-repo or --git-image")
		}

		return false
	}

	return associated && watch
}

// PersistWatch reports whether git-watch should be written onto a replacement.
//
// Only an explicit enabling label is persisted. Process-wide --git-enable
// must keep applying when the label is absent.
//
// Parameters:
//   - c: Container to inspect.
//
// Returns:
//   - bool: True when the source had an explicit on value for git-watch.
func PersistWatch(c types.Container) bool {
	if c == nil {
		return false
	}

	val, ok := c.GetLabel(WatchLabel)
	if !ok {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "t", "true", "yes":
		return true
	default:
		return false
	}
}

// WatchEnabled reports whether the per-container or process-wide Git watcher is on.
//
// A present git-watch label overrides --git-enable. When the label is absent
// or blank, the process-wide default is used.
//
// Parameters:
//   - c: Container to inspect.
//   - params: Update parameters with EnableGitMonitoring.
//
// Returns:
//   - bool: True when the watcher should run, ignoring association.
func WatchEnabled(c types.Container, params types.UpdateParams) bool {
	return isWatch(c, params)
}

// label returns the trimmed container label value for key.
//
// Parameters:
//   - c: Container to inspect.
//   - key: Label key.
//
// Returns:
//   - string: Trimmed value, or empty when the label is missing.
func label(c types.Container, key string) string {
	if c == nil {
		return ""
	}

	val, ok := c.GetLabel(key)
	if !ok {
		return ""
	}

	return strings.TrimSpace(val)
}

// refLabel returns git-ref, falling back to the git-branch alias.
//
// Parameters:
//   - c: Container to inspect.
//
// Returns:
//   - string: Ref label value, or empty.
func refLabel(c types.Container) string {
	return cmp.Or(label(c, RefLabel), label(c, BranchLabel))
}

// policyLabel returns a valid semver policy from the container label.
//
// Parameters:
//   - c: Container to inspect.
//
// Returns:
//   - string: Canonical policy, or empty when unset or invalid.
func policyLabel(c types.Container) string {
	policy := strings.ToLower(label(c, SemverPolicyLabel))
	if types.ValidGitPolicy(policy) {
		return policy
	}

	return ""
}

// imageMapping returns the git-image mapping for an exact ImageName() key.
//
// Parameters:
//   - imageName: Container image name including tag.
//   - images: Process-wide mappings.
//
// Returns:
//   - types.GitImage: Zero value when no exact key exists.
func imageMapping(imageName string, images map[string]types.GitImage) types.GitImage {
	if images == nil || imageName == "" {
		return types.GitImage{}
	}

	return images[imageName]
}

// isWatch reports whether the per-container or process-wide monitor is on.
//
// Parameters:
//   - c: Container to inspect.
//   - params: Update parameters with EnableGitMonitoring.
//
// Returns:
//   - bool: True when the watcher should run, ignoring association.
func isWatch(c types.Container, params types.UpdateParams) bool {
	val, ok := c.GetLabel(WatchLabel)
	if !ok || strings.TrimSpace(val) == "" {
		return params.EnableGitMonitoring
	}

	// Match Docker's loose boolean parsing used elsewhere on Watchtower labels.
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "1", "t", "true", "yes":
		return true
	case "0", "f", "false", "no":
		return false
	default:
		return params.EnableGitMonitoring
	}
}

// defaultRef returns the process default ref, or main.
//
// Parameters:
//   - params: Update parameters.
//
// Returns:
//   - string: Default Git ref.
func defaultRef(params types.UpdateParams) string {
	if params.GitDefaultRef != "" {
		return params.GitDefaultRef
	}

	return "main"
}

// defaultPolicy returns the process default semver policy, or none.
//
// Parameters:
//   - params: Update parameters.
//
// Returns:
//   - string: Canonical policy.
func defaultPolicy(params types.UpdateParams) string {
	if types.ValidGitPolicy(params.GitSemverPolicy) {
		return params.GitSemverPolicy
	}

	return types.GitPolicyNone
}
