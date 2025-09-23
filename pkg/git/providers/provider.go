// Package providers contains Git provider implementations for API optimizations.
package providers

import (
	"net/url"
	"slices"
	"strings"
)

// BaseProvider provides common functionality for all providers.
type BaseProvider struct {
	name  string
	hosts []string
}

// NewBaseProvider creates a new base provider.
//
// Parameters:
//   - name: Provider name (e.g., "github", "gitlab").
//   - hosts: List of supported hostnames.
//
// Returns:
//   - BaseProvider: Configured base provider instance.
func NewBaseProvider(name string, hosts []string) BaseProvider {
	return BaseProvider{
		name:  name,
		hosts: hosts,
	}
}

// Name returns the provider name.
//
// Returns:
//   - string: Provider name (e.g., "github", "gitlab").
func (p BaseProvider) Name() string {
	return p.name
}

// Hosts returns the well-known hostnames for this provider.
//
// Returns:
//   - []string: List of supported hostnames (e.g., ["github.com"]).
func (p BaseProvider) Hosts() []string {
	return p.hosts
}

// IsSupported checks if this provider can handle the given repository URL.
//
// Parameters:
//   - repoURL: Repository URL to check.
//
// Returns:
//   - bool: True if the provider supports this repository URL.
func (p BaseProvider) IsSupported(repoURL string) bool {
	return isHostSupported(repoURL, p.hosts)
}

// isHostSupported checks if the repository URL's host is in the supported hosts list.
func isHostSupported(repoURL string, supportedHosts []string) bool {
	u, err := url.Parse(repoURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Host)

	return slices.Contains(supportedHosts, host)
}
