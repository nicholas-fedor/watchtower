// Package git provides Git repository monitoring capabilities for Watchtower.
//
// This package is organized into subpackages for different Git operations:
//
//   - client: Main Git client for repository operations, commit resolution, and validation
//   - auth: Authentication handling for various Git providers and methods
//   - providers: Provider-specific implementations for GitHub, GitLab, and generic Git support
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
//	import (
//		"github.com/nicholas-fedor/watchtower/pkg/git/client"
//		"github.com/nicholas-fedor/watchtower/pkg/git/auth"
//	)
//
//	gitClient := client.NewClient()
//	authConfig := auth.GetDefaultAuthConfig()
//	commit, err := gitClient.GetLatestCommit(ctx, "https://github.com/user/repo.git", "main", authConfig)
//
// Authentication:
//
// The auth subpackage supports various authentication methods:
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
