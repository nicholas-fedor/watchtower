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

	"github.com/sirupsen/logrus"

	"github.com/nicholas-fedor/watchtower/pkg/git/providers"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Provider implements the GitLab API client.
type Provider struct {
	providers.BaseProvider
	httpClient *http.Client
}

// NewProvider creates a new GitLab provider.
//
// Parameters:
//   - httpClient: HTTP client for making API requests.
//
// Returns:
//   - *Provider: Configured GitLab provider instance.
func NewProvider(httpClient *http.Client) *Provider {
	return &Provider{
		BaseProvider: providers.NewBaseProvider("gitlab", []string{"gitlab.com"}),
		httpClient:   httpClient,
	}
}

// GetLatestCommit retrieves the latest commit hash from GitLab API.
//
// Makes an authenticated request to GitLab's REST API to get the latest commit
// hash for a specific branch, tag, or commit reference. Supports token-based
// authentication for GitLab API access.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - repoURL: GitLab repository URL (e.g., "https://gitlab.com/owner/repo").
//   - ref: Branch name, tag, or commit SHA to resolve.
//   - auth: Authentication configuration for API access.
//
// Returns:
//   - string: Latest commit hash for the specified reference.
//   - error: Non-nil if API request fails or repository/reference is not found.
func (p *Provider) GetLatestCommit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	logrus.WithFields(logrus.Fields{
		"repo": repoURL,
		"ref":  ref,
	}).Debug("GitLab provider: attempting API commit retrieval")

	// Step 1: Parse the GitLab repository URL to extract the project path
	// GitLab uses project paths (group/subgroup/project) instead of owner/repo format
	project, err := parseGitLabRepoURL(repoURL)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitLab provider: failed to parse repository URL")

		return "", err
	}

	// Step 2: Construct the GitLab API URL for the repository commits endpoint
	// Format: https://gitlab.com/api/v4/projects/{project_path}/repository/commits/{ref}
	// The project path is URL-encoded to handle special characters in group/subgroup names
	apiURL := fmt.Sprintf(
		"https://gitlab.com/api/v4/projects/%s/repository/commits/%s",
		project,
		ref,
	)

	logrus.WithFields(logrus.Fields{
		"repo":    repoURL,
		"ref":     ref,
		"api_url": apiURL,
		"project": project,
	}).Debug("GitLab provider: making API request")

	// Step 3: Create the HTTP GET request with context for cancellation/timeout
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitLab provider: failed to create API request")

		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Step 4: Configure authentication headers for GitLab API
	// GitLab uses Private-Token header for API authentication (different from GitHub)
	if auth.Method == types.AuthMethodToken && auth.Token != "" {
		// Personal/project access token authentication
		// Sets "Private-Token: <token>" header (GitLab-specific header name)
		req.Header.Set("Private-Token", auth.Token)
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).Debug("GitLab provider: using token authentication")
	}

	// Step 5: Set required headers for GitLab API requests
	// User-Agent header identifies the client making the request
	req.Header.Set("User-Agent", "Watchtower-Git-Monitor")

	// Step 6: Execute the HTTP request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitLab provider: API network error")

		return "", types.Error{Op: "api", URL: repoURL, Reason: "network error", Cause: err}
	}
	defer resp.Body.Close()

	// Step 7: Handle different HTTP status codes
	if resp.StatusCode == http.StatusNotFound {
		// 404 Not Found: Repository doesn't exist or reference (branch/tag) doesn't exist
		logrus.WithFields(logrus.Fields{
			"repo":        repoURL,
			"ref":         ref,
			"status_code": resp.StatusCode,
		}).Debug("GitLab provider: API returned not found")

		return "", types.Error{Op: "api", URL: repoURL, Reason: "repository or reference not found"}
	}

	if resp.StatusCode != http.StatusOK {
		// Other error status codes (401 Unauthorized, 403 Forbidden, 422 Validation Error, etc.)
		// GitLab returns error details in the response body as plain text
		body, _ := io.ReadAll(resp.Body)

		logrus.WithFields(logrus.Fields{
			"repo":        repoURL,
			"ref":         ref,
			"status_code": resp.StatusCode,
			"error":       string(body),
		}).Debug("GitLab provider: API error response")

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "API error: " + string(body),
		}
	}

	// Step 8: Parse the successful JSON response
	// GitLab API returns commit information in JSON format
	var commitResp commitResponse
	if err := json.NewDecoder(resp.Body).Decode(&commitResp); err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitLab provider: failed to parse API response")

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "failed to parse API response",
			Cause:  err,
		}
	}

	// Step 9: Extract and return the commit ID
	// The commitResp.ID field contains the full 40-character SHA hash
	logrus.WithFields(logrus.Fields{
		"repo":   repoURL,
		"ref":    ref,
		"commit": commitResp.ID,
	}).Debug("GitLab provider: successfully retrieved commit")

	return commitResp.ID, nil
}

// parseGitLabRepoURL extracts project path from GitLab URL.
//
// GitLab URLs can follow various patterns:
// - https://gitlab.com/{group}/{project}
// - https://gitlab.com/{group}/{subgroup}/{project}
// - https://custom.gitlab.com/{group}/{project}
//
// This function extracts the full project path and URL-encodes it for the GitLab API.
//
// Steps:
// 1. Parse the URL to get its components
// 2. Extract the path component (e.g., "/group/subgroup/project")
// 3. Remove leading/trailing slashes and .git suffix
// 4. URL-encode the path for safe API usage
//
// Parameters:
//   - repoURL: GitLab repository URL to parse
//
// Returns:
//   - string: URL-encoded project path for GitLab API
//   - error: Non-nil if URL parsing fails
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
