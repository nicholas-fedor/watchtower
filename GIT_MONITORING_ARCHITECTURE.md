# Git Repository Monitoring Architecture for Watchtower

## Overview

This document outlines the architectural design for adding Git repository monitoring capabilities to Watchtower. The feature extends Watchtower's container update functionality to monitor Git repositories for new commits and trigger container rebuilds, complementing the existing image digest monitoring.

## Requirements

### Core Requirements

- **Git Repository Detection**: Identify containers built from Git repositories
- **Commit Monitoring**: Monitor specified branches/tags for new commits
- **Secure Rebuilding**: Trigger container rebuilds using Docker API (no external commands)
- **State Management**: Track commit hashes without external storage
- **Backwards Compatibility**: Existing Watchtower functionality remains unchanged

### Functional Requirements

- Support for GitHub, GitLab, Bitbucket, and self-hosted Git repositories
- Multiple authentication methods (tokens, SSH keys)
- Semantic versioning policies for updates
- Configurable update intervals and policies
- Comprehensive error handling and logging

## Architecture Decisions

### 1. State Management - Label-Based Storage

**Decision**: Store commit hashes in container labels instead of external databases

- **Rationale**: Mirrors Docker's image digest self-containment model
- **Implementation**: `com.centurylinklabs.watchtower.git-*` label namespace
- **Benefits**: No external dependencies, survives container recreation

### 2. Git Operations - Hybrid API Approach

**Primary Strategy**: Git provider APIs for performance

- GitHub API, GitLab API, etc. for fast, authenticated access
**Fallback Strategy**: go-git library for universal compatibility
- Handles any Git repository regardless of provider
- Robust error handling and authentication

### 3. Rebuild Mechanism - Docker API Only

**Decision**: Use Docker API for image building, no external command execution

- **Security**: Avoids shell execution with elevated Docker privileges
- **Integration**: Leverages existing Watchtower Docker client infrastructure
- **Control**: Full programmatic control over build process

### 4. Integration Model - Container-Level Operations

**Decision**: Operate at individual container level, not docker-compose orchestration

- **Simplicity**: No compose file parsing or manipulation
- **Compatibility**: Works across all deployment methods (compose, swarm, plain Docker)
- **Focus**: Container updates remain Watchtower's core responsibility

## Implementation Strategy

### Core Components

#### `pkg/git/` (New Package)

- `client.go` - Git operations coordinator (API + go-git fallback)
- `auth.go` - Authentication handling (HTTP tokens, SSH keys)
- `types.go` - Git-specific data structures (Repo, Commit, Policy)
- `mocks/` - Test mocks for Git operations

#### `pkg/container/` (Extended)

- `BuildImageFromGit()` - Docker API image building from Git repositories
- Git context handling and build argument injection
- Error handling for build failures

#### `internal/actions/` (Extended)

- Git staleness checking integrated into `update.go`
- Parallel processing with existing image digest checks
- Unified update flow for both monitoring types

#### `internal/flags/` (Extended)

- `--enable-git-monitoring` - Global Git monitoring toggle
- `--git-auth-token` - Default authentication token
- `--git-timeout` - Git operation timeout configuration

#### `pkg/types/` (Extended)

- Git-related fields in `UpdateParams`
- Container interface extensions for Git metadata access
- Policy enumerations and validation

### Update Flow Integration

```text
Container Scanning Loop:
├── Check container labels for Git configuration
├── If Git-monitored:
│   ├── Fetch latest commit via API/go-git
│   ├── Compare with stored commit hash
│   ├── If different:
│   │   ├── Build new image via Docker API
│   │   ├── Update container image reference
│   │   └── Update commit hash label
│   └── Mark for restart
├── If image-monitored: (existing logic)
│   └── Check registry for new digest
└── Execute restarts for stale containers
```

## Security Considerations

### Threat Mitigation

- **No External Commands**: All operations through Docker API
- **Input Validation**: Git URLs and commit hashes validated before use
- **Authentication Security**: Secure credential handling and storage
- **Resource Limits**: Docker build constraints prevent abuse
- **Network Security**: HTTPS-only operations, certificate validation

### Authentication Methods

1. **HTTP Tokens**: GitHub personal access tokens, GitLab tokens
2. **SSH Keys**: Private key authentication for Git operations
3. **Environment Variables**: Secure token passing
4. **Docker Secrets**: Integration with Docker's secret management

## User Experience

### Configuration

```yaml
services:
  webapp:
    build:
      context: https://github.com/user/repo.git
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/repo.git"
      - "com.centurylinklabs.watchtower.git-branch=main"
      - "com.centurylinklabs.watchtower.git-update-policy=minor"
      - "com.centurylinklabs.watchtower.git-commit=abc123"  # Optional: pin to specific commit
```

### Watchtower Configuration

```bash
# Enable Git monitoring globally
WATCHTOWER_GIT_ENABLE=true

# Authentication (can also use WATCHTOWER_GIT_AUTH_TOKEN)
WATCHTOWER_GIT_USERNAME=token
WATCHTOWER_GIT_PASSWORD=github_pat_...

# Optional: SSH key for private repos
WATCHTOWER_GIT_SSH_KEY_PATH=/path/to/key
```

### Update Policies

- `patch` - Only patch updates (1.0.0 → 1.0.1)
- `minor` - Patch + minor updates (1.0.0 → 1.1.0) **[Default]**
- `major` - All updates including breaking changes
- `none` - Manual commit specification only

## Technical Details

### Git Operations Implementation

#### Primary: Provider APIs

```go
// Fast, authenticated access to Git providers
func GetLatestCommitAPI(repoURL, branch, token string) (string, error) {
    // GitHub: GET /repos/{owner}/{repo}/commits/{branch}
    // GitLab: GET /api/v4/projects/{id}/repository/commits/{branch}
    // Returns latest commit SHA
}
```

#### Fallback: go-git Library

```go
// Universal Git support using go-git
func GetLatestCommitGoGit(repoURL, branch string, auth transport.AuthMethod) (string, error) {
    remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
        URLs: []string{repoURL},
    })
    refs, err := remote.List(&git.ListOptions{})
    // Extract commit hash for specified branch
}
```

### Docker API Build Integration

```go
func (c *Client) BuildImageFromGit(ctx context.Context, repoURL, commitHash, imageName string) error {
    buildOptions := types.ImageBuildOptions{
        Remote: repoURL,  // Git URL as build context
        BuildArgs: map[string]*string{
            "GIT_COMMIT": &commitHash,
        },
        Labels: map[string]string{
            "com.centurylinklabs.watchtower.git-commit": commitHash,
        },
        Tags: []string{imageName},
    }

    response, err := c.apiClient.ImageBuild(ctx, nil, buildOptions)
    // Stream and log build output
}
```

### Label Management

- **Repository**: `com.centurylinklabs.watchtower.git-repo`
- **Branch/Tag**: `com.centurylinklabs.watchtower.git-branch`
- **Current Commit**: `com.centurylinklabs.watchtower.git-commit`
- **Update Policy**: `com.centurylinklabs.watchtower.git-update-policy`

### Error Handling

- **Network Failures**: Exponential backoff retry logic
- **Authentication Errors**: Clear error messages, fallback options
- **Build Failures**: Don't update containers, log detailed errors
- **Invalid Git URLs**: Validation with helpful error messages

## Implementation Roadmap

### Phase 1: Core Infrastructure ✅

- [x] Add go-git dependency and basic Git client
- [x] Implement Git provider API clients
- [x] Add Git-related types and interfaces
- [x] Create basic authentication handling

### Phase 2: Container Integration ✅

- [x] Extend container client with Git build capabilities
- [x] Add Git staleness checking to update flow
- [x] Implement label-based state management
- [x] Add Git-related CLI flags

### Phase 3: Update Logic ✅

- [x] Integrate Git checking with existing update logic
- [x] Implement semantic versioning policies
- [x] Add Docker API image building
- [x] Handle build errors and rollback scenarios

### Phase 4: Security & Reliability ✅

- [x] Implement comprehensive authentication methods
- [x] Add timeout and rate limiting
- [x] Security audit and input validation
- [x] Performance optimization

### Phase 5: Testing & Documentation ✅

- [x] Unit tests for Git operations
- [x] Integration tests with real Git repositories
- [x] Documentation and examples
- [x] Update notification templates

## API Design

### Git Client Interface

```go
type GitClient interface {
    GetLatestCommit(repoURL, ref string, auth AuthConfig) (string, error)
    ValidateRepository(repoURL string, auth AuthConfig) error
    ListBranches(repoURL string, auth AuthConfig) ([]string, error)
}
```

### Authentication Configuration

```go
type AuthConfig struct {
    Method   AuthMethod  // token, ssh, basic
    Token    string      // For token-based auth
    Username string      // For basic auth
    Password string      // For basic auth
    SSHKey   []byte      // For SSH key auth
}
```

### Container Extensions

```go
type Container interface {
    // Existing methods...
    IsGitMonitored() bool
    GetGitRepo() string
    GetGitBranch() string
    GetGitCommit() string
    GetGitPolicy() UpdatePolicy
    SetGitCommit(commit string)
}
```

## Testing Strategy

### Unit Tests

- Git client operations (mocked)
- Authentication handling
- Policy validation
- Label parsing

### Integration Tests

- Real Git repository access (public repos)
- Docker API build operations
- End-to-end update scenarios
- Authentication method validation

### Performance Tests

- Concurrent Git operations
- Large repository handling
- Network timeout scenarios
- Memory usage validation

## Migration & Compatibility

### Backwards Compatibility

- Existing Watchtower configurations unchanged
- Git monitoring disabled by default
- No impact on image-based monitoring

### Migration Path

1. **Phase 1**: Feature flag disabled, code integrated
2. **Phase 2**: Feature flag enabled, documentation updated
3. **Phase 3**: Default behavior consideration for future versions

## Success Metrics

- **Functionality**: Successfully monitors and updates Git-based containers
- **Performance**: Git checks complete within timeout windows
- **Security**: No security vulnerabilities introduced
- **Reliability**: Error handling prevents broken container states
- **Usability**: Simple label-based configuration

## Risks & Mitigations

### Technical Risks

- **Git API Changes**: Provider API fallback with go-git
- **Build Failures**: Comprehensive error handling, no forced updates
- **Performance Impact**: Efficient API usage, configurable timeouts

### Security Risks

- **Credential Exposure**: Secure storage, validation, and transmission
- **Command Injection**: No external command execution
- **Resource Exhaustion**: Docker API limits and monitoring

### Operational Risks

- **Network Dependencies**: Graceful degradation on network issues
- **Git Repository Access**: Clear error messages for auth/configuration issues
- **Build Time**: Asynchronous processing, timeout handling

---

This document serves as the comprehensive architectural specification for Git repository monitoring in Watchtower. All implementation decisions should reference this document to ensure consistency and completeness.
