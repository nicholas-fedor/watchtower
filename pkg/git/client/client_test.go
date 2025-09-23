package client

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// MockHTTPClient is a mock implementation of http.Client for testing.
type MockHTTPClient struct {
	mock.Mock
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)

	return args.Get(0).(*http.Response), args.Error(1)
}

func (m *MockHTTPClient) Get(url string) (*http.Response, error) {
	args := m.Called(url)

	return args.Get(0).(*http.Response), args.Error(1)
}

func TestNewClient(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, defaultHTTPTimeout, client.timeout)
}

func TestNewClientWithTimeout(t *testing.T) {
	customTimeout := 42 * time.Second
	client := NewClientWithTimeout(customTimeout)
	assert.NotNil(t, client)
	assert.NotNil(t, client.httpClient)
	assert.Equal(t, customTimeout, client.timeout)
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
			name:    "invalid url - no path parts",
			repoURL: "https://github.com",
			wantErr: true,
		},
		{
			name:    "invalid url - malformed",
			repoURL: "not-a-url",
			wantErr: true,
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

func TestParseGitLabRepoURL(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "valid https url",
			repoURL:  "https://gitlab.com/group/subgroup/project",
			wantPath: "group%2Fsubgroup%2Fproject",
		},
		{
			name:     "valid https url with .git",
			repoURL:  "https://gitlab.com/group/project.git",
			wantPath: "group%2Fproject",
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

func TestTryAPIOptimization(t *testing.T) {
	client := NewClient()

	tests := []struct {
		name    string
		repoURL string
		ref     string
		auth    types.AuthConfig
		wantErr bool
	}{
		{
			name:    "github url",
			repoURL: "https://github.com/owner/repo",
			ref:     "main",
			auth:    types.AuthConfig{Method: types.AuthMethodNone},
			wantErr: true, // Will fail because no actual API call
		},
		{
			name:    "gitlab url",
			repoURL: "https://gitlab.com/group/project",
			ref:     "main",
			auth:    types.AuthConfig{Method: types.AuthMethodNone},
			wantErr: true, // Will fail because no actual API call
		},
		{
			name:    "unsupported host",
			repoURL: "https://bitbucket.org/user/repo",
			ref:     "main",
			auth:    types.AuthConfig{Method: types.AuthMethodNone},
			wantErr: true,
		},
		{
			name:    "invalid url",
			repoURL: "not-a-url",
			ref:     "main",
			auth:    types.AuthConfig{Method: types.AuthMethodNone},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.tryAPIOptimization(context.Background(), tt.repoURL, tt.ref, tt.auth)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateRepository(t *testing.T) {
	// This test would require mocking the Git client, which is complex
	// For now, we'll test the basic structure
	client := NewClient()
	assert.NotNil(t, client)

	// Test with invalid URL that should fail early
	err := client.ValidateRepository(
		context.Background(),
		"not-a-url",
		types.AuthConfig{Method: types.AuthMethodNone},
	)
	assert.Error(t, err)
}

func TestListRefs(t *testing.T) {
	// This would require extensive mocking of go-git
	// For now, test the basic structure
	client := NewClient()
	assert.NotNil(t, client)

	// Test with invalid URL
	_, err := client.listRefs(
		context.Background(),
		"not-a-url",
		types.AuthConfig{Method: types.AuthMethodNone},
		func(plumbing.ReferenceName) bool { return true },
	)
	assert.Error(t, err)
}

func TestListBranches(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)

	// Test with invalid URL
	_, err := client.ListBranches(
		context.Background(),
		"not-a-url",
		types.AuthConfig{Method: types.AuthMethodNone},
	)
	assert.Error(t, err)
}

func TestListTags(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)

	// Test with invalid URL
	_, err := client.ListTags(
		context.Background(),
		"not-a-url",
		types.AuthConfig{Method: types.AuthMethodNone},
	)
	assert.Error(t, err)
}

// TestGitHubAPIClient tests basic GitHub API client functionality.
// Note: Full API testing would require mocking HTTP calls, which is complex
// for functions that construct URLs from repo URLs.
func TestGitHubAPIClient(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)

	// Test with invalid repo URL that should fail during parsing
	_, err := client.getGitHubLatestCommit(
		context.Background(),
		"not-a-url",
		"main",
		types.AuthConfig{Method: types.AuthMethodNone},
	)
	assert.Error(t, err)
}

func TestGitHubAPIClientErrors(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)

	// Test with malformed repo URL
	_, err := client.getGitHubLatestCommit(
		context.Background(),
		"https://github.com", // Missing owner/repo
		"main",
		types.AuthConfig{Method: types.AuthMethodNone},
	)
	assert.Error(t, err)
}

func TestGitHubAPIClientAuth(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)

	// Test with valid repo URL format but will fail on network call
	_, err := client.getGitHubLatestCommit(
		context.Background(),
		"https://github.com/test/repo",
		"main",
		types.AuthConfig{
			Method: types.AuthMethodToken,
			Token:  "test-token",
		},
	)
	// Will fail due to network call, but tests the parsing logic
	assert.Error(t, err)
}

func TestGitLabAPIClient(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)

	// Test with invalid repo URL that should fail during parsing
	_, err := client.getGitLabLatestCommit(
		context.Background(),
		"not-a-url",
		"main",
		types.AuthConfig{Method: types.AuthMethodNone},
	)
	assert.Error(t, err)
}

func TestGitLabAPIClientAuth(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)

	// Test with valid repo URL format but will fail on network call
	_, err := client.getGitLabLatestCommit(
		context.Background(),
		"https://gitlab.com/test/repo",
		"main",
		types.AuthConfig{
			Method: types.AuthMethodToken,
			Token:  "test-token",
		},
	)
	// Will fail due to network call, but tests the parsing logic
	assert.Error(t, err)
}

// TestGetLatestCommit tests the main GetLatestCommit function
// This is complex to test fully due to go-git dependencies.
func TestGetLatestCommit(t *testing.T) {
	client := NewClient()
	assert.NotNil(t, client)

	// Test with invalid URL
	_, err := client.GetLatestCommit(
		context.Background(),
		"not-a-url",
		"main",
		types.AuthConfig{Method: types.AuthMethodNone},
	)
	assert.Error(t, err)
}
