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

	"github.com/sirupsen/logrus"

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
//
// Parameters:
//   - httpClient: HTTP client for making API requests.
//
// Returns:
//   - *Provider: Configured GitHub provider instance.
func NewProvider(httpClient *http.Client) *Provider {
	return &Provider{
		BaseProvider: providers.NewBaseProvider("github", []string{"github.com"}),
		httpClient:   httpClient,
	}
}

// GetLatestCommit retrieves the latest commit hash from GitHub API.
//
// Makes an authenticated request to GitHub's REST API to get the latest commit
// hash for a specific branch, tag, or commit reference. Supports both token
// and basic authentication methods.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - repoURL: GitHub repository URL (e.g., "https://github.com/owner/repo").
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
	}).Debug("GitHub provider: attempting API commit retrieval")

	// Step 1: Parse the GitHub repository URL to extract owner and repository name
	// This is necessary because GitHub API requires owner/repo format in the URL path
	owner, repo, err := parseGitHubRepoURL(repoURL)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitHub provider: failed to parse repository URL")

		return "", err
	}

	// Step 2: Construct the GitHub API URL for the commits endpoint
	// Format: https://api.github.com/repos/{owner}/{repo}/commits/{ref}
	// This endpoint returns commit information for the specified reference (branch/tag/SHA)
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", owner, repo, ref)

	logrus.WithFields(logrus.Fields{
		"repo_url": repoURL,
		"ref":      ref,
		"api_url":  apiURL,
		"owner":    owner,
		"repo":     repo,
	}).Debug("GitHub provider: making API request")

	// Step 3: Create the HTTP GET request with context for cancellation/timeout
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitHub provider: failed to create API request")

		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Step 4: Configure authentication headers based on the provided auth method
	// GitHub supports multiple authentication methods for API access
	if auth.Method == types.AuthMethodToken && auth.Token != "" {
		// Personal Access Token authentication - most common and recommended method
		// Sets "Authorization: token <token>" header
		req.Header.Set("Authorization", "token "+auth.Token)
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).Debug("GitHub provider: using token authentication")
	} else if auth.Method == types.AuthMethodBasic && auth.Username != "" && auth.Password != "" {
		// Basic authentication using username and password/token
		// Sets "Authorization: Basic <base64-encoded-credentials>" header
		// Note: Password can be a personal access token for GitHub
		req.SetBasicAuth(auth.Username, auth.Password)
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).Debug("GitHub provider: using basic authentication")
	}

	// Step 5: Set required headers for GitHub API requests
	// Accept header specifies we want GitHub's v3 REST API JSON response
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	// User-Agent header identifies the client making the request
	req.Header.Set("User-Agent", "Watchtower-Git-Monitor")

	// Step 6: Execute the HTTP request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitHub provider: API network error")

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
		}).Debug("GitHub provider: API returned not found")

		return "", types.Error{Op: "api", URL: repoURL, Reason: "repository or reference not found"}
	}

	if resp.StatusCode != http.StatusOK {
		// Other error status codes (401 Unauthorized, 403 Forbidden, 422 Validation Error, etc.)
		// Read the response body to get detailed error information
		body, _ := io.ReadAll(resp.Body)

		var errorResp errorResponse
		// Attempt to parse the error response as JSON, but don't fail if parsing fails
		_ = json.Unmarshal(body, &errorResp) // Ignore error for fallback parsing

		logrus.WithFields(logrus.Fields{
			"repo":        repoURL,
			"ref":         ref,
			"status_code": resp.StatusCode,
			"error":       errorResp.Message,
		}).Debug("GitHub provider: API error response")

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "API error: " + errorResp.Message,
		}
	}

	// Step 8: Parse the successful JSON response
	// GitHub API returns commit information in JSON format
	var commitResp commitResponse
	if err := json.NewDecoder(resp.Body).Decode(&commitResp); err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitHub provider: failed to parse API response")

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "failed to parse API response",
			Cause:  err,
		}
	}

	// Step 9: Extract and return the commit SHA
	// The commitResp.SHA field contains the full 40-character SHA hash
	logrus.WithFields(logrus.Fields{
		"repo":   repoURL,
		"ref":    ref,
		"commit": commitResp.SHA,
	}).Debug("GitHub provider: successfully retrieved commit")

	return commitResp.SHA, nil
}

// parseGitHubRepoURL extracts owner and repository name from GitHub URL.
//
// GitHub URLs follow the pattern: https://github.com/{owner}/{repo}[.git]
// This function parses the URL and extracts the owner and repository name.
//
// Steps:
// 1. Parse the URL to get its components (scheme, host, path, etc.)
// 2. Split the path by "/" to get individual parts
// 3. Validate that we have at least owner and repo parts
// 4. Remove ".git" suffix from repo name if present
//
// Parameters:
//   - repoURL: GitHub repository URL to parse
//
// Returns:
//   - string: Repository owner/organization name
//   - string: Repository name (without .git suffix)
//   - error: Non-nil if URL parsing fails or format is invalid
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
