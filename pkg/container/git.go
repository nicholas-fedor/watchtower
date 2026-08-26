package container

import (
	"github.com/nicholas-fedor/watchtower/pkg/container/git"
)

const (
	// GitLastCommitLabel is stamped on a replacement container after a git rebuild.
	GitLastCommitLabel = git.LastCommitLabel
	// GitLastTagLabel is stamped on a replacement container after a tag rebuild.
	GitLastTagLabel = git.LastTagLabel
	// DefaultGitDockerfile is used when no Dockerfile path is configured.
	DefaultGitDockerfile = git.DefaultDockerfile
	// DefaultGitContext is the repository root.
	DefaultGitContext = git.DefaultContext
)

// GitAssociation is the monitor association for a container.
type GitAssociation = git.Association

// SetLabel sets a container config label used on the next create.
//
// Parameters:
//   - key: Label key.
//   - value: Label value.
//
// Returns:
//   - none.
func (c *Container) SetLabel(key, value string) {
	if c == nil || key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.containerInfo == nil || c.containerInfo.Config == nil {
		return
	}

	if c.containerInfo.Config.Labels == nil {
		c.containerInfo.Config.Labels = make(map[string]string)
	}

	c.containerInfo.Config.Labels[key] = value
}

// DeleteLabel removes a container config label used on the next create.
//
// Parameters:
//   - key: Label key.
//
// Returns:
//   - none.
func (c *Container) DeleteLabel(key string) {
	if c == nil || key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.containerInfo == nil || c.containerInfo.Config == nil || c.containerInfo.Config.Labels == nil {
		return
	}

	delete(c.containerInfo.Config.Labels, key)
}

// ApplyGitStamp writes git-last-commit and git-last-tag onto container create labels.
//
// Parameters:
//   - c: Concrete container whose create labels will be mutated.
//   - commit: SHA to stamp.
//   - tag: Tag to stamp. Omitted when empty.
func ApplyGitStamp(c *Container, commit, tag string) {
	if c == nil {
		return
	}

	c.SetLabel(git.LastCommitLabel, commit)
	c.DeleteLabel(git.LastTagLabel)

	if tag != "" {
		c.SetLabel(git.LastTagLabel, tag)
	}
}

// ApplyGitAssociation writes monitor association labels onto the replacement container.
//
// This keeps git-image mappings working after Config.Image is rewritten to name:git-<sha>.
//
// Parameters:
//   - c: Concrete container whose create labels will be mutated.
//   - assoc: Resolved repo, ref, policy, and build paths.
//   - watch: When true, persist git-watch=true. False leaves the process-wide
//     --git-enable default in effect on the replacement.
func ApplyGitAssociation(c *Container, assoc GitAssociation, watch bool) {
	if c == nil || assoc.Repo == "" {
		return
	}

	c.SetLabel(git.RepoLabel, assoc.Repo)
	c.DeleteLabel(git.RefLabel)
	c.DeleteLabel(git.SemverPolicyLabel)
	c.DeleteLabel(git.HostLabel)
	c.DeleteLabel(git.DockerfileLabel)
	c.DeleteLabel(git.ContextLabel)
	c.DeleteLabel(git.WatchLabel)

	if assoc.Ref != "" {
		c.SetLabel(git.RefLabel, assoc.Ref)
	}

	if assoc.Policy != "" {
		c.SetLabel(git.SemverPolicyLabel, assoc.Policy)
	}

	if assoc.Host != "" {
		c.SetLabel(git.HostLabel, assoc.Host)
	}

	if assoc.Dockerfile != "" {
		c.SetLabel(git.DockerfileLabel, assoc.Dockerfile)
	}

	if assoc.Context != "" {
		c.SetLabel(git.ContextLabel, assoc.Context)
	}

	if watch {
		c.SetLabel(git.WatchLabel, "true")
	}
}
