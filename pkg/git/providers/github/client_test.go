package github

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

func TestNewProvider(t *testing.T) {
	httpClient := &http.Client{}
	provider := NewProvider(httpClient)

	assert.NotNil(t, provider)
	assert.Equal(t, "github", provider.Name())
	assert.Equal(t, []string{"github.com"}, provider.Hosts())
	assert.Equal(t, httpClient, provider.httpClient)
}

func TestParseGitHubRepoURL(t *testing.T) {
	tests := []struct {
		name      string
		repoURL   string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{
			name:      "valid https url",
			repoURL:   "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "valid https url with .git",
			repoURL:   "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:      "valid http url",
			repoURL:   "http://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
		{
			name:    "invalid url - no path parts",
			repoURL: "https://github.com",
			wantErr: true,
		},
		{
			name:    "invalid url - insufficient path parts",
			repoURL: "https://github.com/owner",
			wantErr: true,
		},
		{
			name:    "invalid url - malformed",
			repoURL: "not-a-url",
			wantErr: true,
		},
		{
			name:      "custom host",
			repoURL:   "https://custom.github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseGitHubRepoURL(tt.repoURL)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantOwner, owner)
				assert.Equal(t, tt.wantRepo, repo)
			}
		})
	}
}

func TestProvider_IsSupported(t *testing.T) {
	httpClient := &http.Client{}
	provider := NewProvider(httpClient)

	tests := []struct {
		name     string
		repoURL  string
		expected bool
	}{
		{
			name:     "github.com https",
			repoURL:  "https://github.com/owner/repo",
			expected: true,
		},
		{
			name:     "not github.com",
			repoURL:  "https://gitlab.com/owner/repo",
			expected: false,
		},
		{
			name:     "invalid url",
			repoURL:  "not-a-url",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.IsSupported(tt.repoURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetLatestCommit tests the main GetLatestCommit function
// Note: Full API testing would require mocking HTTP calls, which is complex.
func TestProvider_GetLatestCommit(t *testing.T) {
	httpClient := &http.Client{}
	provider := NewProvider(httpClient)

	// Test with invalid repo URL that should fail during parsing
	_, err := provider.GetLatestCommit(
		context.TODO(),
		"not-a-url",
		"main",
		types.AuthConfig{Method: types.AuthMethodNone},
	)
	assert.Error(t, err)
}
