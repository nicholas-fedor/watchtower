package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveHostKind verifies the built-in hosts, the extra mappings, and
// hosts written with a port or a trailing dot.
func TestResolveHostKind(t *testing.T) {
	t.Parallel()

	assert.Equal(t, GitHub, ResolveHostKind("github.com", nil))
	assert.Equal(t, GitLab, ResolveHostKind("gitlab.com", nil))
	assert.Equal(t, Gitea, ResolveHostKind("codeberg.org", nil))
	assert.Empty(t, ResolveHostKind("git.example.com", nil))
	assert.Empty(t, ResolveHostKind("bitbucket.org", nil))

	extra := map[string]string{
		"git.example.com":    Gitea,
		"code.local":         Forgejo,
		"gitlab.internal":    GitLab,
		"github.company.com": GitHub,
	}
	assert.Equal(t, Gitea, ResolveHostKind("git.example.com", extra))
	assert.Equal(t, Gitea, ResolveHostKind("code.local", extra))
	assert.Equal(t, GitLab, ResolveHostKind("gitlab.internal", extra))
	assert.Equal(t, GitHub, ResolveHostKind("github.company.com", extra))
	assert.Equal(t, GitHub, ResolveHostKind("github.com", extra))
	assert.Equal(t, Gitea, ResolveHostKind("git.example.com:8443", extra))
	assert.Equal(t, GitHub, ResolveHostKind("github.com.", nil))
	assert.Equal(t, Gitea, ResolveHostKind("git.example.com.", extra))
}

// TestCanonicalHostKind verifies that provider names are matched without
// regard to case and that Forgejo maps to Gitea.
func TestCanonicalHostKind(t *testing.T) {
	t.Parallel()

	assert.Equal(t, GitHub, canonicalHostKind("GitHub"))
	assert.Equal(t, GitLab, canonicalHostKind("gitlab"))
	assert.Equal(t, Gitea, canonicalHostKind("gitea"))
	assert.Equal(t, Gitea, canonicalHostKind("forgejo"))
	assert.Empty(t, canonicalHostKind("bitbucket"))
}
