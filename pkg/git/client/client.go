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
func NewClient() *DefaultClient {
	return &DefaultClient{
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		timeout: defaultHTTPTimeout,
	}
}

// NewClientWithTimeout creates a new Git client with custom timeout.
func NewClientWithTimeout(timeout time.Duration) *DefaultClient {
	return &DefaultClient{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// GetLatestCommit retrieves the latest commit hash for a given reference.
func (c *DefaultClient) GetLatestCommit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	// Primary: Use go-git for universal Git support (works with any Git repo)
	hash, err := c.getLatestCommitGoGit(ctx, repoURL, ref, auth)
	if err == nil {
		return hash, nil
	}

	// Optimization: Try provider APIs for well-known public services
	// This provides faster responses for GitHub/GitLab but isn't required
	if apiHash, apiErr := c.tryAPIOptimization(ctx, repoURL, ref, auth); apiErr == nil {
		return apiHash, nil
	}

	// Return the go-git error if both approaches failed
	return "", err
}

// tryAPIOptimization attempts to use provider APIs for faster responses on well-known services.
func (c *DefaultClient) tryAPIOptimization(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}

	host := strings.ToLower(u.Host)
	switch host {
	case "github.com":
		return c.getGitHubLatestCommit(ctx, repoURL, ref, auth)
	case "gitlab.com":
		return c.getGitLabLatestCommit(ctx, repoURL, ref, auth)
	default:
		// No API optimization available for this host
		return "", fmt.Errorf("%w: %s", ErrNoAPIOptimization, host)
	}
}

// getLatestCommitGoGit uses go-git to get the latest commit.
func (c *DefaultClient) getLatestCommitGoGit(
	ctx context.Context,
	repoURL, ref string,
	auth types.AuthConfig,
) (string, error) {
	authMethod, err := gitAuth.CreateAuthMethod(auth)
	if err != nil {
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
		return "", types.Error{
			Op:     "list",
			URL:    repoURL,
			Reason: "failed to list remote references",
			Cause:  err,
		}
	}

	// Find the reference (branch/tag)
	targetRef := plumbing.NewBranchReferenceName(ref)
	if !strings.Contains(ref, "/") {
		// Try as branch first
		targetRef = plumbing.NewBranchReferenceName(ref)
	}

	// If not found as branch, try as tag
	found := false

	var commitHash plumbing.Hash

	for _, reference := range refs {
		if reference.Name() == targetRef {
			commitHash = reference.Hash()
			found = true

			break
		}
		// Also check for tags
		if reference.Name() == plumbing.NewTagReferenceName(ref) {
			commitHash = reference.Hash()
			found = true

			break
		}
	}

	if !found {
		return "", types.Error{
			Op:     "resolve",
			URL:    repoURL,
			Reason: fmt.Sprintf("reference '%s' not found", ref),
		}
	}

	return commitHash.String(), nil
}

// ValidateRepository checks if a repository exists and is accessible.
func (c *DefaultClient) ValidateRepository(
	ctx context.Context,
	repoURL string,
	auth types.AuthConfig,
) error {
	_, err := c.GetLatestCommit(ctx, repoURL, "HEAD", auth)

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
	authMethod, err := gitAuth.CreateAuthMethod(auth)
	if err != nil {
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

	return result, nil
}

// ListBranches returns available branches for a repository.
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", types.Error{Op: "api", URL: repoURL, Reason: "network error", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", types.Error{Op: "api", URL: repoURL, Reason: "repository or reference not found"}
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)

		var errorResp gitHubErrorResponse

		_ = json.Unmarshal(body, &errorResp) // Ignore error for fallback parsing

		return "", types.Error{
			Op:     "api",
			URL:    repoURL,
			Reason: "API error: " + errorResp.Message,
		}
	}

	var commitResp gitHubCommitResponse
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

	resp, err := c.httpClient.Do(req)
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

	var commitResp gitLabCommitResponse
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
