package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveHostKind verifies that the built-in hosts classify in any
// spelling of the same host and that every other host is unclassified.
func TestResolveHostKind(t *testing.T) {
	t.Parallel()

	assert.Equal(t, GitHub, ResolveHostKind("github.com"))
	assert.Equal(t, GitLab, ResolveHostKind("gitlab.com"))
	assert.Equal(t, Gitea, ResolveHostKind("codeberg.org"))
	assert.Equal(t, GitLab, ResolveHostKind(" [GitLab.com.]:443 "))
	assert.Empty(t, ResolveHostKind("git.example.com"))
	assert.Empty(t, ResolveHostKind("git.example.com:8443"))
	assert.Empty(t, ResolveHostKind("bitbucket.org"))
}

// TestHostname verifies the spellings that name the same host.
func TestHostname(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "lowercase", host: "git.example.com", want: "git.example.com"},
		{name: "uppercase", host: "Git.Example.COM", want: "git.example.com"},
		{name: "trailing dot", host: "GitHub.com.", want: "github.com"},
		{name: "surrounding spaces", host: " git.example.com ", want: "git.example.com"},
		{name: "bracketed IPv6", host: "[2001:DB8::1]", want: "2001:db8::1"},
		{name: "bare IPv6", host: "2001:db8::1", want: "2001:db8::1"},
		{name: "bracketed name", host: "[git.example.com]", want: "git.example.com"},
		{name: "port", host: "git.example.com:3000", want: "git.example.com"},
		{name: "bracketed IPv6 with port", host: "[2001:db8::1]:443", want: "2001:db8::1"},
		{name: "fully qualified with port", host: "GitHub.com.:443", want: "github.com"},
		{name: "empty", host: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Hostname(tt.host))
		})
	}
}
