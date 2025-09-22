// Package git provides Git repository monitoring capabilities for Watchtower.
//
// This package enables Watchtower to monitor Git repositories for new commits,
// supporting both Git provider APIs (for performance) and go-git (for universal compatibility).
// It handles authentication, commit resolution, and repository validation.
//
// Key Features:
//   - Hybrid API/go-git approach for optimal performance and compatibility
//   - Support for GitHub, GitLab, and other Git providers
//   - Multiple authentication methods (tokens, SSH keys, basic auth)
//   - Semantic versioning policy support
//   - Comprehensive error handling with detailed GitError types
//
// Usage:
//
//	import "github.com/nicholas-fedor/watchtower/pkg/git"
//
//	client := git.NewClient()
//	commit, err := client.GetLatestCommit(ctx, "https://github.com/user/repo.git", "main", authConfig)
//
// Authentication:
//
// The package supports various authentication methods:
//   - Token authentication (GitHub/GitLab personal access tokens)
//   - SSH key authentication
//   - Basic username/password authentication
//   - No authentication for public repositories
//
// Error Handling:
//
// The package provides detailed error information through GitError types:
//   - Authentication failures
//   - Network errors
//   - Repository access issues
//   - Invalid references
//
// Use IsAuthError() and IsNetworkError() helper functions to categorize errors
// for appropriate retry logic and user feedback.
package git
