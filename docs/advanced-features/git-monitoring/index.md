# Git Repository Monitoring

Watchtower can monitor Git repositories for new commits and automatically rebuild containers when changes are detected. This feature complements the existing image digest monitoring by enabling continuous deployment from Git repositories.

## Overview

Git monitoring allows Watchtower to:

- Monitor Git repositories for new commits on specified branches or tags
- Automatically rebuild Docker images using the Docker API
- Update running containers with the new images
- Support multiple Git providers (GitHub, GitLab, Bitbucket, and self-hosted)
- Use various authentication methods for private repositories

## Quick Start

### Basic Configuration

To enable Git monitoring for a container, add the following labels:

```yaml
services:
  webapp:
    image: myapp:latest
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/myapp.git"
      - "com.centurylinklabs.watchtower.git-branch=main"
    build:
      context: https://github.com/user/myapp.git
```

### Enable Git Monitoring

Enable Git monitoring globally:

```bash
docker run -d \
  --name watchtower \
  --volume /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_GIT_ENABLE=true \
  nickfedor/watchtower
```

Or using Docker Compose:

```yaml
version: '3.8'
services:
  watchtower:
    image: nickfedor/watchtower
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      - WATCHTOWER_GIT_ENABLE=true
    restart: unless-stopped
```

## Container Labels

Configure Git monitoring for individual containers using these labels:

| Label | Description | Required | Default |
|-------|-------------|----------|---------|
| `com.centurylinklabs.watchtower.git-repo` | Git repository URL | Yes | - |
| `com.centurylinklabs.watchtower.git-branch` | Branch or tag to monitor | No | `main` |
| `com.centurylinklabs.watchtower.git-update-policy` | Update policy (patch/minor/major/none) | No | `minor` |
| `com.centurylinklabs.watchtower.git-commit` | Pin to specific commit hash | No | Latest |

### Examples

#### Monitor Main Branch

```yaml
services:
  app:
    image: myapp:latest
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/app.git"
      - "com.centurylinklabs.watchtower.git-branch=main"
```

#### Monitor Specific Tag

```yaml
services:
  app:
    image: myapp:v1.0
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/app.git"
      - "com.centurylinklabs.watchtower.git-branch=v1.0"
```

#### Pin to Specific Commit

```yaml
services:
  app:
    image: myapp:stable
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/app.git"
      - "com.centurylinklabs.watchtower.git-branch=main"
      - "com.centurylinklabs.watchtower.git-commit=abc123def456"
```

## Authentication

### Public Repositories

No authentication required for public repositories:

```yaml
services:
  app:
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/public-repo.git"
```

### Private Repositories

Several authentication methods are supported for private repositories.

#### Personal Access Tokens (Recommended)

For GitHub and GitLab, use personal access tokens:

```yaml
services:
  watchtower:
    environment:
      - WATCHTOWER_GIT_USERNAME=token
      - WATCHTOWER_GIT_PASSWORD=github_pat_xxxxxxxxxx
```

Alternatively, these environment variables can be replaced with the --git-username and --git-password command-line flags.

#### SSH Keys

For SSH-based authentication:

```yaml
services:
  watchtower:
    volumes:
      - /path/to/ssh/key:/root/.ssh/id_rsa:ro
    environment:
      - WATCHTOWER_GIT_SSH_KEY_PATH=/root/.ssh/id_rsa
```

#### OAuth App Flow (Recommended for Organizations)

For reduced rate limiting and better access to private repositories, register Watchtower as an OAuth app:

##### GitHub OAuth App Setup

1. Go to GitHub Settings → Developer settings → OAuth Apps
2. Click "New OAuth App"
3. Fill in application details:
   - **Application name**: Watchtower
   - **Homepage URL**: <https://github.com/nicholas-fedor/watchtower>
   - **Authorization callback URL**: Leave blank (not used)
4. Create the OAuth app and note the **Client ID**
5. Generate a **Client Secret**
6. Use the client credentials for authentication:

```bash
docker run -d \
  --name watchtower \
  -e WATCHTOWER_GIT_ENABLE=true \
  -e WATCHTOWER_GIT_USERNAME=<client_id> \
  -e WATCHTOWER_GIT_PASSWORD=<client_secret> \
  -v /var/run/docker.sock:/var/run/docker.sock \
  nickfedor/watchtower
```

##### GitLab OAuth App Setup

1. Go to GitLab → User Settings → Applications
2. Create a new application:
   - **Name**: Watchtower
   - **Redirect URI**: Leave blank or use a placeholder
   - **Scopes**: Check `api` scope
3. Note the **Application ID** and **Secret**
4. Use for authentication:

```bash
docker run -d \
  --name watchtower \
  -e WATCHTOWER_GIT_ENABLE=true \
  -e WATCHTOWER_GIT_USERNAME=<application_id> \
  -e WATCHTOWER_GIT_PASSWORD=<secret> \
  -v /var/run/docker.sock:/var/run/docker.sock \
  nickfedor/watchtower
```

!!! tip "OAuth Benefits"
    - **Higher Rate Limits**: OAuth apps get higher API rate limits than personal tokens
    - **Organization Access**: Better access to private organization repositories
    - **Token Rotation**: Easier to rotate client credentials than personal tokens
    - **Audit Trail**: Better tracking of API usage

## Authentication Priority

Authentication is configured globally via command-line flags and environment variables. When multiple global authentication methods are configured, Watchtower uses a priority system to determine which method to use for Git operations. This ensures predictable behavior and prevents conflicts when different authentication methods are available.

### Priority Order

Authentication methods are prioritized as follows (highest to lowest):

1. **Token Authentication** (Personal access tokens, OAuth app credentials)
2. **Basic Authentication** (Username/password)
3. **SSH Key Authentication**
4. **No Authentication** (Public repositories only)

### Multiple Methods Behavior

- Only the **highest priority** authentication method that is configured and valid will be used
- Lower priority methods are ignored when a higher priority method is available
- If the highest priority method fails (e.g., invalid token), Watchtower will not fall back to lower priority methods

### User Guidance

To avoid confusion and ensure reliable authentication:

- **Configure only one authentication method** globally
- **Use token authentication** when possible, as it provides the best security and rate limit benefits
- **Test authentication** before deploying to production
- **Monitor logs** for authentication-related messages

!!! warning "Authentication Conflicts"
    Configuring multiple methods may lead to unexpected behavior. Always verify which method is being used in the logs.

### Auth Prioritization Examples

#### Single Method (Recommended)

```yaml
services:
  watchtower:
    environment:
      - WATCHTOWER_GIT_ENABLE=true
      - WATCHTOWER_GIT_USERNAME=token
      - WATCHTOWER_GIT_PASSWORD=github_pat_xxxxxxxxxx
```

Only token authentication is used.

#### Multiple Methods Configured (Token Takes Priority)

```yaml
services:
  watchtower:
    volumes:
      - /path/to/ssh/key:/root/.ssh/id_rsa:ro
    environment:
      - WATCHTOWER_GIT_ENABLE=true
      - WATCHTOWER_GIT_USERNAME=token
      - WATCHTOWER_GIT_PASSWORD=github_pat_xxxxxxxxxx
      - WATCHTOWER_GIT_SSH_KEY_PATH=/root/.ssh/id_rsa
```

Token authentication is used; SSH key is ignored.

## Update Policies

Control when containers are updated based on semantic versioning:

| Policy | Description | Example Updates |
|--------|-------------|-----------------|
| `patch` | Only patch updates | 1.0.0 → 1.0.1 |
| `minor` | Patch + minor updates | 1.0.0 → 1.1.0 |
| `major` | All updates | 1.0.0 → 2.0.0 |
| `none` | Manual commit specification only | No automatic updates |

```yaml
services:
  app:
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/app.git"
      - "com.centurylinklabs.watchtower.git-update-policy=patch"
```

## Configuration Options

### Global Flags

| Flag | Environment Variable | Description | Default |
|------|---------------------|-------------|---------|
| `--enable-git-monitoring` | `WATCHTOWER_GIT_ENABLE` | Enable Git monitoring globally | `false` |
| `--git-auth-token` | `WATCHTOWER_GIT_AUTH_TOKEN` | Default authentication token | - |
| `--git-timeout` | `WATCHTOWER_GIT_TIMEOUT` | Git operation timeout | `30s` |

### Environment Variables

```bash
# Enable Git monitoring
WATCHTOWER_GIT_ENABLE=true

# Authentication (global)
WATCHTOWER_GIT_USERNAME=token
WATCHTOWER_GIT_PASSWORD=github_pat_xxxxxxxxxx

# SSH authentication
WATCHTOWER_GIT_SSH_KEY_PATH=/path/to/ssh/key

# Timeout configuration
WATCHTOWER_GIT_TIMEOUT=60s
```

## Advanced Usage

### Multiple Branches

Monitor different branches for different environments:

```yaml
services:
  app-staging:
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/app.git"
      - "com.centurylinklabs.watchtower.git-branch=develop"

  app-production:
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://github.com/user/app.git"
      - "com.centurylinklabs.watchtower.git-branch=main"
      - "com.centurylinklabs.watchtower.git-update-policy=patch"
```

### Self-Hosted Git

Works with self-hosted GitLab, Gitea, and other Git providers:

```yaml
services:
  app:
    labels:
      - "com.centurylinklabs.watchtower.git-repo=https://git.company.com/user/app.git"
      - "com.centurylinklabs.watchtower.git-branch=main"
```

### Docker Secrets

Use Docker secrets for sensitive authentication data:

```yaml
secrets:
  git_token:
    file: git_token.txt

services:
  watchtower:
    secrets:
      - git_token
    environment:
      - WATCHTOWER_GIT_PASSWORD=/run/secrets/git_token
```

## Troubleshooting

### Common Issues

#### Authentication Failures

- Verify token permissions (repo scope required)
- Check token expiration
- Ensure correct username/token format

#### Rate Limiting

- Use OAuth app credentials for higher limits
- Implement polling intervals to reduce API calls
- Monitor Git provider rate limit headers

#### Build Failures

- Ensure Docker build context is accessible
- Check build logs for errors
- Verify Git repository structure

#### Permission Issues

- Confirm Docker socket access
- Check SSH key permissions (600)
- Verify OAuth app scopes

### Debug Mode

Enable debug logging for troubleshooting:

```bash
docker run -d \
  --name watchtower \
  -e WATCHTOWER_DEBUG=true \
  -e WATCHTOWER_GIT_ENABLE=true \
  -v /var/run/docker.sock:/var/run/docker.sock \
  nickfedor/watchtower
```

### Logs

Monitor Watchtower logs for Git monitoring activity:

```bash
docker logs -f watchtower
```

Look for log entries like:

```text
time="2024-01-01T12:00:00Z" level=info msg="Found Git-monitored container: app"
time="2024-01-01T12:00:00Z" level=info msg="Git repository updated: abc123... → def456..."
time="2024-01-01T12:00:00Z" level=info msg="Built new image from Git: app:latest"
```

## Security Considerations

- Store authentication tokens securely (Docker secrets, environment variables)
- Use OAuth apps for organizational deployments
- Regularly rotate credentials
- Monitor API usage and rate limits
- Keep SSH keys secure with proper permissions

## Performance Notes

- Git API calls are made per monitored container
- OAuth apps provide higher rate limits than personal tokens
- Consider polling intervals to balance responsiveness vs. API usage
- Build operations may take time depending on repository size

## Compatibility

- **Git Providers**: GitHub, GitLab, Bitbucket, Gitea, self-hosted Git
- **Authentication**: Personal tokens, OAuth apps, SSH keys, basic auth
- **Docker**: Requires Docker API access for image building
- **Networks**: HTTPS-only for security (SSH for Git access)
