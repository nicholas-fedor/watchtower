// Package providers contains Git provider implementations for API optimizations.
package providers

import (
	"slices"
	"net/url"
	"strings"
)

// BaseProvider provides common functionality for all providers.
type BaseProvider struct {
	name  string
	hosts []string
}

// NewBaseProvider creates a new base provider.
func NewBaseProvider(name string, hosts []string) BaseProvider {
	return BaseProvider{
		name:  name,
		hosts: hosts,
	}
}

// Name returns the provider name.
func (p BaseProvider) Name() string {
	return p.name
}

// Hosts returns the well-known hostnames for this provider.
func (p BaseProvider) Hosts() []string {
	return p.hosts
}

// IsSupported checks if this provider can handle the given repository URL.
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
