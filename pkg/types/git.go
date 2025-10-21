// Package types provides shared types for Git operations in Watchtower.
package types

import (
	"context"
	"errors"
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

// AuthMethod represents different authentication methods for Git operations.
type AuthMethod string

const (
	// AuthMethodNone indicates no authentication.
	AuthMethodNone AuthMethod = "none"

	// AuthMethodToken indicates token-based authentication (GitHub/GitLab tokens).
	AuthMethodToken AuthMethod = "token"

	// AuthMethodBasic indicates username/password authentication.
	AuthMethodBasic AuthMethod = "basic"

	// AuthMethodSSH indicates SSH key authentication.
	AuthMethodSSH AuthMethod = "ssh"
)

// AuthConfig holds authentication configuration for Git operations.
type AuthConfig struct {
	Method   AuthMethod
	Token    string
	Username string
	Password string
	SSHKey   []byte
}

// Client defines the interface for Git operations.
type Client interface {
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

// Error represents a Git operation error with structured information.
type Error struct {
	Op     string // Operation that failed
	URL    string // Repository URL
	Reason string // Human-readable reason
	Cause  error  // Underlying error
}

func (e Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("git %s %s: %s: %v", e.Op, e.URL, e.Reason, e.Cause)
	}

	return fmt.Sprintf("git %s %s: %s", e.Op, e.URL, e.Reason)
}

func (e Error) Unwrap() error {
	return e.Cause
}

// IsAuthError checks if an error is authentication-related.
func IsAuthError(err error) bool {
	var gitErr Error
	if errors.As(err, &gitErr) {
		return gitErr.Reason == "authentication failed" ||
			gitErr.Reason == "repository not found" ||
			gitErr.Reason == "access denied"
	}

	return false
}

// IsNetworkError checks if an error is network-related.
func IsNetworkError(err error) bool {
	var gitErr Error
	if errors.As(err, &gitErr) {
		return gitErr.Reason == "network error" ||
			gitErr.Reason == "timeout"
	}

	return false
}

// Provider defines the interface for Git provider API clients.
type Provider interface {
	// Name returns the provider name (e.g., "github", "gitlab").
	Name() string

	// Hosts returns the well-known hostnames for this provider.
	Hosts() []string

	// GetLatestCommit retrieves the latest commit hash using provider-specific API.
	GetLatestCommit(ctx context.Context, repoURL, ref string, auth AuthConfig) (string, error)

	// IsSupported checks if this provider can handle the given repository URL.
	IsSupported(repoURL string) bool
}
