// Package gitlab provides GitLab-specific API client for Git operations.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/watchtower/pkg/git/providers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Provider implements the GitLab API client.
type Provider struct {
	providers.BaseProvider
	httpClient *http.Client
}

// NewProvider creates a new GitLab provider.
func NewProvider(httpClient *http.Client) *Provider {
	return &Provider{
		BaseProvider: providers.NewBaseProvider("gitlab", []string{"gitlab.com"}),
		httpClient:   httpClient,
	}
}

// GetLatestCommit retrieves the latest commit hash from GitLab API.
func (p *Provider) GetLatestCommit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	// Parse GitLab repository from URL
	project, err := parseGitLabRepoURL(repoURL)
	if err != nil {
		return "", err
	}

	apiURL := fmt.Sprintf(
		"https://gitlab.com/api/v4/projects/%s/repository/commits/%s",
		project,
		ref,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add authentication if provided
	if auth.Method == types.AuthMethodToken && auth.Token != "" {
		req.Header.Set("Private-Token", auth.Token)
	}

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

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "API error: " + string(body),
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

	return commitResp.ID, nil
}

// parseGitLabRepoURL extracts project path from GitLab URL.
func parseGitLabRepoURL(repoURL string) (string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse repository URL: %w", err)
	}

	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")

	// URL encode for GitLab API
	return url.PathEscape(path), nil
}

// GitLab API response structures.
type commitResponse struct {
	ID string `json:"id"`
}
