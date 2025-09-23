// Package github provides GitHub-specific API client for Git operations.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/watchtower/pkg/git/providers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Minimum number of path parts required for a valid repository URL.
const minPathParts = 2

// Predefined error variables for consistent error handling.
var (
	ErrInvalidURLPath = errors.New("URL path must have at least 2 parts")
)

// Provider implements the GitHub API client.
type Provider struct {
	providers.BaseProvider
	httpClient *http.Client
}

// NewProvider creates a new GitHub provider.
func NewProvider(httpClient *http.Client) *Provider {
	return &Provider{
		BaseProvider: providers.NewBaseProvider("github", []string{"github.com"}),
		httpClient:   httpClient,
	}
}

// GetLatestCommit retrieves the latest commit hash from GitHub API.
func (p *Provider) GetLatestCommit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	// Parse GitHub repository from URL
	owner, repo, err := parseGitHubRepoURL(repoURL)
	if err != nil {
		return "", err
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", owner, repo, ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add authentication if provided
	if auth.Method == types.AuthMethodToken && auth.Token != "" {
		req.Header.Set("Authorization", "token "+auth.Token)
	} else if auth.Method == types.AuthMethodBasic && auth.Username != "" && auth.Password != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Watchtower-Git-Monitor")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", types.Error{Op: "api", URL: repoURL, Reason: "network error", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", types.Error{Op: "api", URL: repoURL, Reason: "repository or reference not found"}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		var errorResp errorResponse

		_ = json.Unmarshal(body, &errorResp) // Ignore error for fallback parsing

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "API error: " + errorResp.Message,
		}
	}

	var commitResp commitResponse
	if err := json.NewDecoder(resp.Body).Decode(&commitResp); err != nil {
		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "failed to parse API response",
			Cause:  err,
		}
	}

	return commitResp.SHA, nil
}

// parseGitHubRepoURL extracts owner and repo from GitHub URL.
func parseGitHubRepoURL(repoURL string) (string, string, error) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse repository URL: %w", err)
	}

	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	if len(pathParts) < minPathParts {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidURLPath, parsedURL.Path)
	}

	return pathParts[0], strings.TrimSuffix(pathParts[1], ".git"), nil
}

// GitHub API response structures.
type commitResponse struct {
	SHA string `json:"sha"`
}

type errorResponse struct {
	Message string `json:"message"`
}
