// Package client provides Git client operations for Watchtower's Git monitoring feature.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/sirupsen/logrus"

	gitAuth "github.com/nicholas-fedor/watchtower/pkg/git/auth"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// Default timeout for HTTP requests.
const defaultHTTPTimeout = 30 * time.Second

// Minimum number of path parts required for a valid repository URL.
const minPathParts = 2

// Predefined error variables for consistent error handling.
var (
	ErrNoAPIOptimization = errors.New("no API optimization available for host")
	ErrInvalidURL        = errors.New("invalid repository URL")
	ErrInvalidURLPath    = errors.New("URL path must have at least 2 parts")
)

// DefaultClient implements the GitClient interface.
type DefaultClient struct {
	httpClient *http.Client
	timeout    time.Duration
}

// NewClient creates a new Git client with default settings.
//
// It initializes a client with default HTTP timeout and standard configuration
// suitable for most Git operations.
//
// Returns:
//   - *DefaultClient: Configured Git client instance.
func NewClient() *DefaultClient {
	return &DefaultClient{
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		timeout: defaultHTTPTimeout,
	}
}

// NewClientWithTimeout creates a new Git client with custom timeout.
//
// Parameters:
//   - timeout: Custom timeout duration for HTTP operations.
//
// Returns:
//   - *DefaultClient: Configured Git client instance with specified timeout.
func NewClientWithTimeout(timeout time.Duration) *DefaultClient {
	return &DefaultClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// GetLatestCommit retrieves the latest commit hash for a given reference.
//
// It implements a hybrid approach: first attempts to use go-git for universal Git support,
// then falls back to provider-specific APIs (GitHub, GitLab) for faster responses.
// This ensures compatibility with any Git repository while optimizing performance for well-known providers.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - repoURL: URL of the Git repository.
//   - ref: Branch name, tag, or commit hash to resolve.
//   - auth: Authentication configuration for repository access.
//
// Returns:
//   - string: Latest commit hash for the specified reference.
//   - error: Non-nil if commit retrieval fails from both go-git and API approaches.
func (c *DefaultClient) GetLatestCommit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	fields := logrus.Fields{
		"repo": repoURL,
		"ref":  ref,
		"auth": auth.Method,
	}

	logrus.WithFields(fields).Debug("Starting commit retrieval")

	// Primary: Use go-git for universal Git support (works with any Git repo)
	hash, err := c.getLatestCommitGoGit(ctx, repoURL, ref, auth)
	if err == nil {
		logrus.WithFields(fields).
			WithField("commit", hash).
			Debug("Successfully retrieved commit via go-git")

		return hash, nil
	}

	logrus.WithFields(fields).
		WithError(err).
		Debug("go-git retrieval failed, trying API optimization")

	// Optimization: Try provider APIs for well-known public services
	// This provides faster responses for GitHub/GitLab but isn't required
	if apiHash, apiErr := c.tryAPIOptimization(ctx, repoURL, ref, auth); apiErr == nil {
		logrus.WithFields(fields).
			WithField("commit", apiHash).
			Debug("Successfully retrieved commit via API")

		return apiHash, nil
	}

	logrus.WithFields(fields).WithError(err).Debug("Both go-git and API approaches failed")
	// Return the go-git error if both approaches failed
	return "", err
}

// tryAPIOptimization attempts to use provider APIs for faster responses on well-known services.
func (c *DefaultClient) tryAPIOptimization(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("Failed to parse repository URL for API optimization")

		return "", fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}

	host := strings.ToLower(parsedURL.Host)
	logrus.WithFields(logrus.Fields{
		"repo": repoURL,
		"ref":  ref,
		"host": host,
	}).Debug("Attempting API optimization")

	switch host {
	case "github.com":
		return c.getGitHubLatestCommit(ctx, repoURL, ref, auth)
	case "gitlab.com":
		return c.getGitLabLatestCommit(ctx, repoURL, ref, auth)
	default:
		// No API optimization available for this host
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
			"host": host,
		}).Debug("No API optimization available for host")

		return "", fmt.Errorf("%w: %s", ErrNoAPIOptimization, host)
	}
}

// getLatestCommitGoGit uses go-git to get the latest commit.
func (c *DefaultClient) getLatestCommitGoGit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	logrus.WithFields(logrus.Fields{
		"repo": repoURL,
		"ref":  ref,
	}).Debug("Attempting go-git commit retrieval")

	authMethod, err := gitAuth.CreateAuthMethod(auth)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
			"auth": auth.Method,
		}).WithError(err).Debug("Authentication setup failed")

		return "", types.Error{
			Op:     "auth",
			URL:    repoURL,
			Reason: "authentication setup failed",
			Cause:  err,
		}
	}

	// Create remote for ls-remote equivalent
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	})

	// List references (equivalent to git ls-remote)
	refs, err := remote.ListContext(ctx, &git.ListOptions{
		Auth: authMethod,
	})
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("Failed to list remote references")

		return "", types.Error{
			Op:     "list",
			URL:    repoURL,
			Reason: "failed to list remote references",
			Cause:  err,
		}
	}

	// Find the reference (branch/tag)
	// Git references can be branches (refs/heads/branch), tags (refs/tags/tag), or full refs
	var targetRef plumbing.ReferenceName
	if !strings.Contains(ref, "/") {
		// For simple names without slashes, assume it's a branch name
		// This handles common cases like "main", "master", "develop"
		targetRef = plumbing.NewBranchReferenceName(ref)
	} else {
		// For refs with slashes, use as-is (e.g., "refs/heads/feature-branch")
		targetRef = plumbing.ReferenceName(ref)
	}

	// Search through all remote references to find the target
	// Git repositories can have multiple reference types: branches, tags, remotes, etc.
	found := false

	var commitHash plumbing.Hash

	for _, reference := range refs {
		// First, try exact match with the constructed target reference
		// This handles branches (refs/heads/branch) and full refs (refs/tags/tag)
		if reference.Name() == targetRef {
			commitHash = reference.Hash()
			found = true

			break
		}

		// Fallback: Check if the original ref name matches a tag
		// This handles cases where user provides "v1.0.0" and it should match "refs/tags/v1.0.0"
		// Only do this if we haven't already found a match
		if !found && reference.Name() == plumbing.NewTagReferenceName(ref) {
			commitHash = reference.Hash()
			found = true

			break
		}
	}

	if !found {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).Debug("Reference not found in remote")

		return "", types.Error{
			Op:     "resolve",
			URL:    repoURL,
			Reason: fmt.Sprintf("reference '%s' not found", ref),
		}
	}

	hash := commitHash.String()
	logrus.WithFields(logrus.Fields{
		"repo":   repoURL,
		"ref":    ref,
		"commit": hash,
	}).Debug("Successfully retrieved commit via go-git")

	return hash, nil
}

// ValidateRepository checks if a repository exists and is accessible.
//
// It attempts to retrieve the HEAD commit to verify repository accessibility
// and authentication configuration.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - repoURL: URL of the Git repository to validate.
//   - auth: Authentication configuration for repository access.
//
// Returns:
//   - error: Non-nil if repository is inaccessible or authentication fails.
func (c *DefaultClient) ValidateRepository(
	ctx context.Context,
	repoURL string,
	auth types.AuthConfig,
) error {
	logrus.WithFields(logrus.Fields{
		"repo": repoURL,
		"auth": auth.Method,
	}).Debug("Validating repository accessibility")

	_, err := c.GetLatestCommit(ctx, repoURL, "HEAD", auth)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
		}).WithError(err).Debug("Repository validation failed")
	} else {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
		}).Debug("Repository validation successful")
	}

	return err
}

// listRefs retrieves references from a Git repository and filters them by type.
// It uses Git protocol to list remote references without cloning the repository.
func (c *DefaultClient) listRefs(
	ctx context.Context,
	repoURL string,
	auth types.AuthConfig,
	filterFunc func(plumbing.ReferenceName) bool,
) ([]string, error) {
	logrus.WithFields(logrus.Fields{
		"repo": repoURL,
		"auth": auth.Method,
	}).Debug("Listing remote references")

	authMethod, err := gitAuth.CreateAuthMethod(auth)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"auth": auth.Method,
		}).WithError(err).Debug("Authentication setup failed for listing refs")

		return nil, types.Error{
			Op:     "auth",
			URL:    repoURL,
			Reason: "authentication setup failed",
			Cause:  err,
		}
	}

	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{repoURL},
	})

	refs, err := remote.ListContext(ctx, &git.ListOptions{
		Auth: authMethod,
	})
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
		}).WithError(err).Debug("Failed to list remote references")

		return nil, types.Error{
			Op:     "list",
			URL:    repoURL,
			Reason: "failed to list references",
			Cause:  err,
		}
	}

	var result []string

	for _, ref := range refs {
		if filterFunc(ref.Name()) {
			result = append(result, ref.Name().Short())
		}
	}

	logrus.WithFields(logrus.Fields{
		"repo":  repoURL,
		"count": len(result),
		"total": len(refs),
	}).Debug("Successfully listed and filtered references")

	return result, nil
}

// ListBranches returns available branches for a repository.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - repoURL: URL of the Git repository.
//   - auth: Authentication configuration for repository access.
//
// Returns:
//   - []string: List of branch names (without refs/heads/ prefix).
//   - error: Non-nil if branch listing fails.
func (c *DefaultClient) ListBranches(
	ctx context.Context,
	repoURL string,
	auth types.AuthConfig,
) ([]string, error) {
	return c.listRefs(ctx, repoURL, auth, func(name plumbing.ReferenceName) bool {
		return name.IsBranch()
	})
}

// ListTags returns available tags for a repository.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control.
//   - repoURL: URL of the Git repository.
//   - auth: Authentication configuration for repository access.
//
// Returns:
//   - []string: List of tag names (without refs/tags/ prefix).
//   - error: Non-nil if tag listing fails.
func (c *DefaultClient) ListTags(
	ctx context.Context,
	repoURL string,
	auth types.AuthConfig,
) ([]string, error) {
	return c.listRefs(ctx, repoURL, auth, func(name plumbing.ReferenceName) bool {
		return name.IsTag()
	})
}

// GitHub API structures.
type gitHubCommitResponse struct {
	SHA string `json:"sha"`
}

type gitHubErrorResponse struct {
	Message string `json:"message"`
}

// getGitHubLatestCommit gets the latest commit from GitHub API.
func (c *DefaultClient) getGitHubLatestCommit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	logrus.WithFields(logrus.Fields{
		"repo": repoURL,
		"ref":  ref,
	}).Debug("Attempting GitHub API commit retrieval")

	// Parse GitHub repository from URL
	owner, repo, err := parseGitHubRepoURL(repoURL)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("Failed to parse GitHub repository URL")

		return "", err
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", owner, repo, ref)

	logrus.WithFields(logrus.Fields{
		"repo_url": repoURL,
		"ref":      ref,
		"api_url":  apiURL,
		"owner":    owner,
		"repo":     repo,
	}).Debug("Making GitHub API request")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("Failed to create GitHub API request")

		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add authentication if provided
	// GitHub supports both personal access tokens and basic auth (username + password)
	if auth.Method == types.AuthMethodToken && auth.Token != "" {
		// GitHub personal access token authentication
		// Format: "Authorization: token <token>"
		req.Header.Set("Authorization", "token "+auth.Token)
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).Debug("Using token authentication for GitHub API")
	} else if auth.Method == types.AuthMethodBasic && auth.Username != "" && auth.Password != "" {
		// Basic authentication using username and password/token
		// Sets "Authorization: Basic <base64-encoded-credentials>"
		req.SetBasicAuth(auth.Username, auth.Password)
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).Debug("Using basic authentication for GitHub API")
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Watchtower-Git-Monitor")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitHub API network error")

		return "", types.Error{Op: "api", URL: repoURL, Reason: "network error", Cause: err}
	}
	defer resp.Body.Close()

	// Handle different HTTP status codes
	if resp.StatusCode == http.StatusNotFound {
		// 404 Not Found: Repository or reference doesn't exist
		logrus.WithFields(logrus.Fields{
			"repo":        repoURL,
			"ref":         ref,
			"status_code": resp.StatusCode,
		}).Debug("GitHub API returned not found")

		return "", types.Error{Op: "api", URL: repoURL, Reason: "repository or reference not found"}
	}

	if resp.StatusCode != http.StatusOK {
		// Other error status codes (401, 403, 422, etc.)
		// Try to parse the error message from response body
		body, _ := io.ReadAll(resp.Body)

		var errorResp gitHubErrorResponse

		_ = json.Unmarshal(body, &errorResp) // Ignore unmarshal error for fallback

		// Log the error details for debugging
		logrus.WithFields(logrus.Fields{
			"repo":        repoURL,
			"ref":         ref,
			"status_code": resp.StatusCode,
			"error":       errorResp.Message,
		}).Debug("GitHub API error response")

		// Return a structured error with the API error message
		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "API error: " + errorResp.Message,
		}
	}

	var commitResp gitHubCommitResponse
	if err := json.NewDecoder(resp.Body).Decode(&commitResp); err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("Failed to parse GitHub API response")

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "failed to parse API response",
			Cause:  err,
		}
	}

	logrus.WithFields(logrus.Fields{
		"repo":   repoURL,
		"ref":    ref,
		"commit": commitResp.SHA,
	}).Debug("Successfully retrieved commit from GitHub API")

	return commitResp.SHA, nil
}

// parseGitHubRepoURL extracts owner and repo from GitHub URL.
//
// GitHub URLs follow the pattern: https://github.com/{owner}/{repo}[.git]
// This function parses the URL and extracts the owner and repository name.
//
// Steps:
// 1. Parse the URL to get its components
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
	// Parse the URL to extract its components (scheme, host, path, etc.)
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Split the path by "/" and trim leading/trailing slashes
	// Example: "/owner/repo.git" becomes ["owner", "repo.git"]
	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")

	// GitHub URLs must have at least owner and repo parts
	if len(pathParts) < minPathParts {
		return "", "", fmt.Errorf("%w: %s", ErrInvalidURLPath, parsedURL.Path)
	}

	// Extract owner and repo, removing .git suffix from repo name
	owner := pathParts[0]
	repo := strings.TrimSuffix(pathParts[1], ".git")

	return owner, repo, nil
}

// GitLab API structures.
type gitLabCommitResponse struct {
	ID string `json:"id"`
}

// getGitLabLatestCommit gets the latest commit from GitLab API.
func (c *DefaultClient) getGitLabLatestCommit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	logrus.WithFields(logrus.Fields{
		"repo": repoURL,
		"ref":  ref,
	}).Debug("Attempting GitLab API commit retrieval")

	// Parse GitLab repository from URL
	project, err := parseGitLabRepoURL(repoURL)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("Failed to parse GitLab repository URL")

		return "", err
	}

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
	}).Debug("Making GitLab API request")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("Failed to create GitLab API request")

		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add authentication if provided
	// GitLab uses Private-Token header for API authentication
	if auth.Method == types.AuthMethodToken && auth.Token != "" {
		// GitLab personal/project access token authentication
		// Format: "Private-Token: <token>"
		req.Header.Set("Private-Token", auth.Token)
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).Debug("Using token authentication for GitLab API")
	}

	req.Header.Set("User-Agent", "Watchtower-Git-Monitor")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("GitLab API network error")

		return "", types.Error{Op: "api", URL: repoURL, Reason: "network error", Cause: err}
	}
	defer resp.Body.Close()

	// Handle different HTTP status codes
	if resp.StatusCode == http.StatusNotFound {
		// 404 Not Found: Repository or reference doesn't exist
		logrus.WithFields(logrus.Fields{
			"repo":        repoURL,
			"ref":         ref,
			"status_code": resp.StatusCode,
		}).Debug("GitLab API returned not found")

		return "", types.Error{Op: "api", URL: repoURL, Reason: "repository or reference not found"}
	}

	if resp.StatusCode != http.StatusOK {
		// Other error status codes (401, 403, 422, etc.)
		// GitLab returns error details in the response body as plain text
		body, _ := io.ReadAll(resp.Body)

		// Log the error details for debugging
		logrus.WithFields(logrus.Fields{
			"repo":        repoURL,
			"ref":         ref,
			"status_code": resp.StatusCode,
			"error":       string(body),
		}).Debug("GitLab API error response")

		// Return a structured error with the API error message
		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "API error: " + string(body),
		}
	}

	var commitResp gitLabCommitResponse
	if err := json.NewDecoder(resp.Body).Decode(&commitResp); err != nil {
		logrus.WithFields(logrus.Fields{
			"repo": repoURL,
			"ref":  ref,
		}).WithError(err).Debug("Failed to parse GitLab API response")

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "failed to parse API response",
			Cause:  err,
		}
	}

	logrus.WithFields(logrus.Fields{
		"repo":   repoURL,
		"ref":    ref,
		"commit": commitResp.ID,
	}).Debug("Successfully retrieved commit from GitLab API")

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
	// Parse the URL to extract its components
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse repository URL: %w", err)
	}

	// Extract the path and clean it up
	// Example: "/group/subgroup/project.git" becomes "group/subgroup/project"
	path := strings.Trim(parsedURL.Path, "/") // Remove leading/trailing slashes
	path = strings.TrimSuffix(path, ".git")   // Remove .git suffix if present

	// GitLab API requires URL encoding for project paths with special characters
	// Example: "group/subgroup/project" becomes "group%2Fsubgroup%2Fproject"
	return url.PathEscape(path), nil
}
