package container

import (
	"maps"

	dockerContainer "github.com/moby/moby/api/types/container"

	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/pkg/container/git"
)

// DefaultGitDockerfile is used when no Dockerfile path is configured.
const DefaultGitDockerfile = git.DefaultDockerfile

// gitAssociation is the monitor association for a container.
type gitAssociation = git.Association

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

	c.replaceInspectLocked(func(info *dockerContainer.InspectResponse) {
		labels := maps.Clone(info.Config.Labels)
		if labels == nil {
			labels = make(map[string]string, 1)
		}

		labels[key] = value
		info.Config.Labels = labels
	})
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

	if c.containerInfo == nil || c.containerInfo.Config == nil {
		return
	}

	if _, ok := c.containerInfo.Config.Labels[key]; !ok {
		return
	}

	c.replaceInspectLocked(func(info *dockerContainer.InspectResponse) {
		labels := maps.Clone(info.Config.Labels)
		delete(labels, key)
		info.Config.Labels = labels
	})
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

	c.SetLabel(labels.GitLastCommit, commit)
	c.DeleteLabel(labels.GitLastTag)

	if tag != "" {
		c.SetLabel(labels.GitLastTag, tag)
	}
}

// ApplyGitAssociation writes monitor association labels onto the replacement container.
//
// This keeps git-image mappings working after Config.Image is rewritten to name:git-<sha>.
// Ref, policy, Dockerfile, and context labels are written only when a label or
// image mapping supplied them. Process defaults stay off the replacement.
//
// Parameters:
//   - c: Concrete container whose create labels will be mutated.
//   - assoc: Resolved repo, ref, policy, and build paths.
//   - watch: When true, persist git-watch=true. False leaves the process-wide
//     --git-enable default in effect on the replacement.
func ApplyGitAssociation(c *Container, assoc gitAssociation, watch bool) {
	if c == nil || assoc.Repo == "" {
		return
	}

	repo := redactGitURL(assoc.Repo)

	c.DeleteLabel(labels.GitRepo)
	c.DeleteLabel(labels.GitRef)
	c.DeleteLabel(labels.GitSemverPolicy)
	c.DeleteLabel(labels.GitHost)
	c.DeleteLabel(labels.GitDockerfile)
	c.DeleteLabel(labels.GitContext)
	c.DeleteLabel(labels.GitWatch)

	if repo == "" {
		return
	}

	c.SetLabel(labels.GitRepo, repo)

	if assoc.Ref != "" && !assoc.RefFromDefault {
		c.SetLabel(labels.GitRef, assoc.Ref)
	}

	if assoc.Policy != "" && !assoc.PolicyFromDefault {
		c.SetLabel(labels.GitSemverPolicy, assoc.Policy)
	}

	if assoc.Host != "" {
		if host := redactGitURL(assoc.Host); host != "" {
			c.SetLabel(labels.GitHost, host)
		}
	}

	if assoc.Dockerfile != "" && !assoc.DockerfileFromDefault {
		c.SetLabel(labels.GitDockerfile, assoc.Dockerfile)
	}

	if assoc.Context != "" && !assoc.ContextFromDefault {
		c.SetLabel(labels.GitContext, assoc.Context)
	}

	if watch {
		c.SetLabel(labels.GitWatch, "true")
	}
}
