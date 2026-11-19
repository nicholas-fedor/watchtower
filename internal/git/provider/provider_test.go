package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveHostKind verifies the built-in hosts, hosts written with a
// port, brackets, a trailing dot or uppercase letters, and that every other
// host is unclassified.
func TestResolveHostKind(t *testing.T) {
	t.Parallel()

	assert.Equal(t, GitHub, ResolveHostKind("github.com"))
	assert.Equal(t, GitLab, ResolveHostKind("gitlab.com"))
	assert.Equal(t, Gitea, ResolveHostKind("codeberg.org"))
	assert.Equal(t, GitHub, ResolveHostKind("GitHub.com:443"))
	assert.Equal(t, GitHub, ResolveHostKind("github.com."))
	assert.Equal(t, GitLab, ResolveHostKind("[gitlab.com]"))
	assert.Empty(t, ResolveHostKind("git.example.com"))
	assert.Empty(t, ResolveHostKind("git.example.com:8443"))
	assert.Empty(t, ResolveHostKind("bitbucket.org"))
}
