package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewBaseProvider(t *testing.T) {
	name := "test-provider"
	hosts := []string{"example.com", "test.com"}

	provider := NewBaseProvider(name, hosts)

	assert.Equal(t, name, provider.Name())
	assert.Equal(t, hosts, provider.Hosts())
}

func TestBaseProvider_Name(t *testing.T) {
	name := "github"
	hosts := []string{"github.com"}
	provider := NewBaseProvider(name, hosts)

	assert.Equal(t, name, provider.Name())
}

func TestBaseProvider_Hosts(t *testing.T) {
	name := "github"
	hosts := []string{"github.com", "api.github.com"}
	provider := NewBaseProvider(name, hosts)

	assert.Equal(t, hosts, provider.Hosts())
}

func TestBaseProvider_IsSupported(t *testing.T) {
	tests := []struct {
		name     string
		hosts    []string
		repoURL  string
		expected bool
	}{
		{
			name:     "supported host - https",
			hosts:    []string{"github.com"},
			repoURL:  "https://github.com/owner/repo",
			expected: true,
		},
		{
			name:     "unsupported host",
			hosts:    []string{"github.com"},
			repoURL:  "https://gitlab.com/owner/repo",
			expected: false,
		},
		{
			name:     "multiple hosts - first match",
			hosts:    []string{"github.com", "gitlab.com"},
			repoURL:  "https://github.com/owner/repo",
			expected: true,
		},
		{
			name:     "multiple hosts - second match",
			hosts:    []string{"github.com", "gitlab.com"},
			repoURL:  "https://gitlab.com/owner/repo",
			expected: true,
		},
		{
			name:     "case insensitive",
			hosts:    []string{"github.com"},
			repoURL:  "https://GITHUB.COM/owner/repo",
			expected: true,
		},
		{
			name:     "invalid URL",
			hosts:    []string{"github.com"},
			repoURL:  "not-a-url",
			expected: false,
		},
		{
			name:     "empty URL",
			hosts:    []string{"github.com"},
			repoURL:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewBaseProvider("test", tt.hosts)
			result := provider.IsSupported(tt.repoURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsHostSupported(t *testing.T) {
	tests := []struct {
		name           string
		repoURL        string
		supportedHosts []string
		expected       bool
	}{
		{
			name:           "exact match",
			repoURL:        "https://github.com/owner/repo",
			supportedHosts: []string{"github.com"},
			expected:       true,
		},
		{
			name:           "no match",
			repoURL:        "https://github.com/owner/repo",
			supportedHosts: []string{"gitlab.com"},
			expected:       false,
		},
		{
			name:           "multiple hosts match",
			repoURL:        "https://github.com/owner/repo",
			supportedHosts: []string{"gitlab.com", "github.com"},
			expected:       true,
		},
		{
			name:           "case insensitive match",
			repoURL:        "https://GITHUB.COM/owner/repo",
			supportedHosts: []string{"github.com"},
			expected:       true,
		},
		{
			name:           "invalid URL",
			repoURL:        "not-a-url",
			supportedHosts: []string{"github.com"},
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isHostSupported(tt.repoURL, tt.supportedHosts)
			assert.Equal(t, tt.expected, result)
		})
	}
}
