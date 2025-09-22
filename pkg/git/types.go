// Package git provides Git repository operations for Watchtower's Git monitoring feature.
// It supports both Git provider APIs (for performance) and go-git (for universal compatibility).
package git

import (
	"context"
	"fmt"
	"time"
)

// UpdatePolicy defines when Git updates should be applied.
type UpdatePolicy string

const (
	// PolicyPatch only allows patch updates (1.0.0 → 1.0.1).
	PolicyPatch UpdatePolicy = "patch"
	// PolicyMinor allows patch and minor updates (1.0.0 → 1.1.0).
	PolicyMinor UpdatePolicy = "minor"
	// PolicyMajor allows all updates including breaking changes.
	PolicyMajor UpdatePolicy = "major"
	// PolicyNone disables automatic updates, manual commit specification only.
	PolicyNone UpdatePolicy = "none"
)

// AuthMethod defines the authentication method for Git operations.
type AuthMethod string

const (
	// AuthMethodToken uses HTTP token authentication.
	AuthMethodToken AuthMethod = "token"
	// AuthMethodSSH uses SSH key authentication.
	AuthMethodSSH AuthMethod = "ssh"
	// AuthMethodBasic uses username/password authentication.
	AuthMethodBasic AuthMethod = "basic"
	// AuthMethodNone uses no authentication (public repos only).
	AuthMethodNone AuthMethod = "none"
)

// AuthConfig contains authentication configuration for Git operations.
type AuthConfig struct {
	Method   AuthMethod // Authentication method
	Token    string     // For token-based auth
	Username string     // For basic auth
	Password string     // For basic auth
	SSHKey   []byte     // For SSH key auth
}

// GitClient defines the interface for Git operations.
type GitClient interface {
	// GetLatestCommit retrieves the latest commit hash for a given reference
	GetLatestCommit(ctx context.Context, repoURL, ref string, auth AuthConfig) (string, error)

	// ValidateRepository checks if a repository exists and is accessible
	ValidateRepository(ctx context.Context, repoURL string, auth AuthConfig) error

	// ListBranches returns available branches for a repository
	ListBranches(ctx context.Context, repoURL string, auth AuthConfig) ([]string, error)

	// ListTags returns available tags for a repository
	ListTags(ctx context.Context, repoURL string, auth AuthConfig) ([]string, error)
}

// CommitInfo contains information about a Git commit.
type CommitInfo struct {
	Hash      string    // Commit hash
	Message   string    // Commit message
	Author    string    // Author name
	Timestamp time.Time // Commit timestamp
}

// RepositoryInfo contains metadata about a Git repository.
type RepositoryInfo struct {
	URL           string   // Repository URL
	DefaultBranch string   // Default branch name
	Branches      []string // Available branches
	Tags          []string // Available tags
}

// GitError represents Git-specific errors.
type GitError struct {
	Op     string // Operation that failed
	URL    string // Repository URL
	Reason string // Human-readable reason
	Cause  error  // Underlying error
}

func (e GitError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("git %s failed for %s: %s: %v", e.Op, e.URL, e.Reason, e.Cause)
	}

	return fmt.Sprintf("git %s failed for %s: %s", e.Op, e.URL, e.Reason)
}

func (e GitError) Unwrap() error {
	return e.Cause
}

// IsAuthError checks if an error is authentication-related.
func IsAuthError(err error) bool {
	if gitErr, ok := err.(GitError); ok {
		return gitErr.Reason == "authentication failed" ||
			gitErr.Reason == "repository not found" ||
			gitErr.Reason == "access denied"
	}

	return false
}

// IsNetworkError checks if an error is network-related.
func IsNetworkError(err error) bool {
	if gitErr, ok := err.(GitError); ok {
		return gitErr.Reason == "network error" ||
			gitErr.Reason == "timeout"
	}

	return false
}
