package container

import (
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
	"github.com/nicholas-fedor/watchtower/internal/container/redact"
	"github.com/nicholas-fedor/watchtower/pkg/container/git"
)

// DefaultGitDockerfile is used when no Dockerfile path is configured.
const DefaultGitDockerfile = git.DefaultDockerfile

// gitAssociation is the monitor association for a container.
type gitAssociation = git.Association

// ApplyGitStamp writes git-last-commit and git-last-tag onto container create labels.
//
// Parameters:
//   - c: Concrete container whose create labels will be mutated.
//   - commit: SHA to stamp.
//   - tag: Tag to stamp. Omitted when empty.
func ApplyGitStamp(c *container.Container, commit, tag string) {
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
func ApplyGitAssociation(c *container.Container, assoc gitAssociation, watch bool) {
	if c == nil || assoc.Repo == "" {
		return
	}

	repo := redact.GitURL(assoc.Repo)

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
		if host := redact.GitURL(assoc.Host); host != "" {
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
