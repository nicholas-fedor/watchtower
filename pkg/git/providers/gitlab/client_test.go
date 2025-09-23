package gitlab

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
	assert.Equal(t, "gitlab", provider.Name())
	assert.Equal(t, []string{"gitlab.com"}, provider.Hosts())
	assert.Equal(t, httpClient, provider.httpClient)
}

func TestParseGitLabRepoURL(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "valid https url",
			repoURL:  "https://gitlab.com/group/project",
			wantPath: "group%2Fproject",
		},
		{
			name:     "valid https url with .git",
			repoURL:  "https://gitlab.com/group/project.git",
			wantPath: "group%2Fproject",
		},
		{
			name:     "valid https url with subgroup",
			repoURL:  "https://gitlab.com/group/subgroup/project",
			wantPath: "group%2Fsubgroup%2Fproject",
		},
		{
			name:     "valid http url",
			repoURL:  "http://gitlab.com/group/project",
			wantPath: "group%2Fproject",
		},
		{
			name:     "custom host",
			repoURL:  "https://custom.gitlab.com/group/project",
			wantPath: "group%2Fproject",
		},
		{
			name:     "empty path after trimming",
			repoURL:  "https://gitlab.com/",
			wantPath: "",
		},
		{
			name:     "empty path after trimming",
			repoURL:  "https://gitlab.com/",
			wantPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := parseGitLabRepoURL(tt.repoURL)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPath, path)
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
			name:     "gitlab.com https",
			repoURL:  "https://gitlab.com/group/project",
			expected: true,
		},
		{
			name:     "not gitlab.com",
			repoURL:  "https://github.com/owner/repo",
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
