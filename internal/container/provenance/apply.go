package provenance

import (
	"github.com/nicholas-fedor/watchtower/internal/container"
	"github.com/nicholas-fedor/watchtower/internal/container/labels"
)

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

// ApplyGitAssociation writes the labels [Association.Labels] returns onto a
// container that is about to be recreated.
//
// Every association label on the container is removed first. A label the new
// association leaves out does not survive from the old one. A container
// without a repository is left unchanged.
//
// Parameters:
//   - c: Concrete container whose create labels will be mutated.
//   - assoc: Resolved repo, ref, policy, and build paths.
func ApplyGitAssociation(c *container.Container, assoc Association) {
	if c == nil || assoc.Repo == "" {
		return
	}

	for _, key := range associationLabelKeys {
		c.DeleteLabel(key)
	}

	for key, value := range assoc.Labels() {
		c.SetLabel(key, value)
	}
}
