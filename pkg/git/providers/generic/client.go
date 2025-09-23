// Package generic provides a universal Git client using go-git for any Git repository.
package generic

import (
	"context"

	"github.com/nicholas-fedor/watchtower/pkg/git/providers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Provider implements a universal Git client using go-git.
type Provider struct {
	providers.BaseProvider
}

// NewProvider creates a new generic Git provider.
func NewProvider() *Provider {
	return &Provider{
		BaseProvider: providers.NewBaseProvider("generic", []string{}), // Supports all hosts
	}
}

// GetLatestCommit retrieves the latest commit hash using go-git.
// This provider supports any Git repository URL.
func (p *Provider) GetLatestCommit(
	_ context.Context,
	repoURL, _ string,
	_ types.AuthConfig,
) (string, error) {
	// This provider doesn't provide API optimizations - it always returns an error
	// to indicate that go-git should be used as the fallback
	return "", types.Error{
		Op:     "generic",
		URL:    repoURL,
		Reason: "generic provider delegates to go-git",
	}
}

// IsSupported returns true for any valid Git repository URL.
// This makes it the fallback provider for all repositories.
func (p *Provider) IsSupported(_ string) bool {
	// The generic provider supports everything that looks like a Git URL
	// but doesn't match specific providers like GitHub/GitLab
	return true
}
