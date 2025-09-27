# Arguments

## Overview

By default, Watchtower monitors all containers running on the Docker daemon it connects to (typically the local daemon, configurable via the `--host` flag).
To limit monitoring to specific containers, provide their names as arguments when starting Watchtower.

```bash
docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    --restart unless-stopped \
    nickfedor/watchtower \
    nginx redis
```

In this example, Watchtower monitors only the "nginx" and "redis" containers, ignoring others. To run a single update attempt and exit, use the `--run-once` flag with the `--rm` option to remove the Watchtower container afterward.

```bash
docker run --rm \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    --run-once \
    nginx redis
```

This command triggers an update attempt for "nginx" and "redis" containers, displays debug output, and removes the Watchtower container upon completion. Without arguments, Watchtower monitors all running containers.

## Secrets/Files

Certain flags support referencing a file, using its contents as the value, to securely handle sensitive data like passwords or tokens, avoiding exposure in configuration files or command lines.

| Flag                                   | Environment Variable                            |
|----------------------------------------|-------------------------------------------------|
| `--http-api-token`                     | `WATCHTOWER_HTTP_API_TOKEN`                     |
| `--notification-email-server-password` | `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD` |
| `--notification-gotify-token`          | `WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN`          |
| `--notification-msteams-hook`          | `WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK`          |
| `--notification-slack-hook-url`        | `WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL`        |
| `--notification-url`                   | `WATCHTOWER_NOTIFICATION_URL`                   |

### Example Docker Compose Usage

```yaml
secrets:
  access_token:
    file: access_token

services:
  watchtower:
    secrets:
      - access_token
    environment:
      - WATCHTOWER_HTTP_API_TOKEN=/run/secrets/access_token
```

## Time Zone

Sets the time zone for Watchtower's logs and the `--schedule` flag's cron expressions.
Without this setting, Watchtower defaults to UTC.

To specify a time zone, use a value from the [TZ Database](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones){target="_blank" rel="noopener noreferrer"} (e.g., `Europe/Rome`).
Alternatively, mount the host's `/etc/localtime` file using `-v /etc/localtime:/etc/localtime:ro`.

```text
            Argument: None
Environment Variable: TZ
                Type: String
             Default: UTC
```

## General Options

### Help

Displays documentation for supported flags.

```text
            Argument: --help
Environment Variable: N/A
                Type: N/A
             Default: N/A
```

### Debug

Enables debug mode with verbose logging.

```text
            Argument: --debug, -d
Environment Variable: WATCHTOWER_DEBUG
                Type: Boolean
             Default: false
```

!!! Note
    Equivalent to `--log-level debug`.
    As an argument, it does not accept a value (e.g., `--debug true` is invalid).

    See [Maximum Log Level](#maximum_log_level).

### Trace

Enables trace mode with highly verbose logging, including sensitive information like credentials.

```text
            Argument: --trace
Environment Variable: WATCHTOWER_TRACE
                Type: Boolean
             Default: false
```

!!! Note
    Equivalent to `--log-level trace`.
    As an argument, does not accept a value (e.g., `--trace true` is invalid).

    See [Maximum Log Level](#maximum_log_level).

!!! Warning
    Use with caution due to credential exposure.

### Maximum Log Level

Sets the maximum log level output to STDERR, visible in `docker logs` when running Watchtower in a container.

```text
            Argument: --log-level
Environment Variable: WATCHTOWER_LOG_LEVEL
     Possible Values: panic, fatal, error, warn, info, debug, trace
             Default: info
```

### Logging Format

Specifies the format for console log output.

```text
            Argument: --log-format, -l
Environment Variable: WATCHTOWER_LOG_FORMAT
     Possible Values: Auto, LogFmt, Pretty, JSON
             Default: Auto
```

### Disable ANSI Colors

Disables ANSI color escape codes in log output for plain text logs.

```text
            Argument: --no-color
Environment Variable: NO_COLOR
                Type: Boolean
             Default: false
```

### Run Once

Triggers a single update attempt for specified containers and exits immediately.

```text
            Argument: --run-once, -R
Environment Variable: WATCHTOWER_RUN_ONCE
                Type: Boolean
             Default: false
!!! Warning
    When using `--run-once` with Docker Compose or similar orchestration tools, ensure your container's restart policy is compatible. Using `restart: unless-stopped` or similar policies may cause restart loops after Watchtower exits successfully. Consider using `restart: "no"` or `--rm` with `docker run` for one-time updates.
```

!!! Note
    Enables debug output during execution, suitable for interactive use.
    Use with `--rm` to remove the Watchtower container after completion.

### Update on Start

Performs an update check on startup.
If a schedule is configured (via --schedule or --interval), then Watchtower continues with periodic updates.

```text
            Argument: --update-on-start
Environment Variable: WATCHTOWER_UPDATE_ON_START
                Type: Boolean
             Default: false
```

!!! Note
    If used with `--run-once`, a warning is logged and `--run-once` takes precedence.

## Scheduling & Polling

### Schedule

Defines when and how often Watchtower checks for new images using a 6-field [Cron expression](https://pkg.go.dev/github.com/robfig/cron@v1.2.0?tab=doc#hdr-CRON_Expression_Format){target="_blank" rel="noopener noreferrer"}.

Example: `--schedule "0 0 4 * * *"` runs daily at 4 AM.

```text
            Argument: --schedule, -s
Environment Variable: WATCHTOWER_SCHEDULE
                Type: String
             Default: None
```

!!! Note
    Cannot be used with `--interval`.

    Requires a time zone set via `TZ` or a mounted `/etc/localtime` file.
    See [Time Zone](#time_zone).

### Poll Interval

Sets the interval (in seconds) for polling new images.

```text
            Argument: --interval, -i
Environment Variable: WATCHTOWER_POLL_INTERVAL
                Type: Integer
             Default: 86400 (24 hours)
```

!!! Note
    Cannot be used with `--schedule`.
    Overrides cron-based scheduling.

### HTTP API Periodic Polls

Enables periodic updates when HTTP API mode is active, allowing both API-triggered and scheduled updates.

```text
            Argument: --http-api-periodic-polls
Environment Variable: WATCHTOWER_HTTP_API_PERIODIC_POLLS
                Type: Boolean
             Default: false
```

!!! Note
    Requires `--http-api-update`.

    See [HTTP API Mode](#http_api_mode).

## Container Management

### Include Stopped Containers

Includes created and exited containers in monitoring and updates.

```text
            Argument: --include-stopped, -S
Environment Variable: WATCHTOWER_INCLUDE_STOPPED
                Type: Boolean
             Default: false
```

### Revive Stopped Containers

Restarts stopped containers after their images are updated.

```text
            Argument: --revive-stopped
Environment Variable: WATCHTOWER_REVIVE_STOPPED
                Type: Boolean
             Default: false
```

!!! Note
    Requires `--include-stopped`.

### Include Restarting Containers

Includes containers in the restarting state for monitoring and updates.

```text
            Argument: --include-restarting
Environment Variable: WATCHTOWER_INCLUDE_RESTARTING
                Type: Boolean
             Default: false
```

### Disable Container Restart

Prevents restarting containers after updating.
This is useful when an external system (e.g., systemd) manages the container lifecycle.

```text
            Argument: --no-restart
Environment Variable: WATCHTOWER_NO_RESTART
                Type: Boolean
             Default: false
```

!!! Warning
    Combining `--no-restart` with `--cleanup` during Watchtower self-update may leave a renamed Watchtower container running without starting a new one, preventing cleanup of the old image.

    Use cautiously for self-updating Watchtower instances and consider external lifecycle management (e.g., Docker Compose) to restart containers manually.

### Rolling Restart

Restarts containers one at a time to minimize downtime.
This is ideal for zero-downtime deployments with lifecycle hooks.
When containers have health checks configured, Watchtower waits for each container to become healthy before proceeding to the next one.

```text
            Argument: --rolling-restart
Environment Variable: WATCHTOWER_ROLLING_RESTART
                Type: Boolean
             Default: false
```

!!! Note
    When combined with `--cleanup`, image cleanup is deferred until all containers are updated, which may temporarily increase disk usage for large numbers of containers (>50).
    This is typically negligible for homelab setups but monitor disk space on resource-constrained hosts.

    If a container fails to become healthy within 5 minutes, Watchtower logs a warning but continues with the next container to avoid blocking the entire update process.

### Cleanup Old Images

Removes old images after updating containers to free disk space.

```text
            Argument: --cleanup
Environment Variable: WATCHTOWER_CLEANUP
                Type: Boolean
             Default: false
```

!!! Note
    During Watchtower self-updates, cleanup is deferred to the new container to prevent premature image deletion.

    Ensure `--no-restart` is not used with `--cleanup` to avoid incomplete updates.

### Remove Anonymous Volumes

Deletes anonymous volumes when updating containers.
Named volumes remain unaffected.

```text
            Argument: --remove-volumes
Environment Variable: WATCHTOWER_REMOVE_VOLUMES
                Type: Boolean
             Default: false
```

!!! Note
    Containers with the Docker `AutoRemove` option enabled are automatically removed by the Docker daemon after stopping.
    Watchtower skips explicit removal in such cases.
    This does not affect named volumes.

### Monitor Only

Monitors for new images, sends notifications, and runs lifecycle hooks without updating containers.

```text
            Argument: --monitor-only
Environment Variable: WATCHTOWER_MONITOR_ONLY
                Type: Boolean
             Default: false
```

!!! Note
    Images may still be pulled due to Docker API limitations for digest comparison.

    Can be set per container via the `com.centurylinklabs.watchtower.monitor-only` label.

    See [Label Precedence](#label_precedence).

### Disable Self-Update

Disables self-update of the Watchtower container.

```text
            Argument: --no-self-update
Environment Variable: WATCHTOWER_NO_SELF_UPDATE
                Type: Boolean
             Default: false
```

### Disable Image Pulling

Prevents pulling new images from registries, monitoring only local image cache changes.
Useful for locally built images.

```text
            Argument: --no-pull
Environment Variable: WATCHTOWER_NO_PULL
                Type: Boolean
             Default: false
```

!!! Note
    Can be set per container via the `com.centurylinklabs.watchtower.no-pull` label.

    See [Label Precedence](#label_precedence).

### Enable Label Filter

Restricts monitoring to containers with the `com.centurylinklabs.watchtower.enable` label set to `true` when the `--label-enable` flag is specified.
Without `--label-enable`, containers with this label set to `false` are excluded, while others are monitored by default.

```text
            Argument: --label-enable
Environment Variable: WATCHTOWER_LABEL_ENABLE
                Type: Boolean
             Default: false
```

!!! Note
    When `--label-enable` is unset, containers without the `com.centurylinklabs.watchtower.enable` label or with it set to `true` are monitored, and those with `false` are excluded.

    When `--label-enable` is set, only containers with `true` are monitored, ignoring those with `false` or no label.

### Disable Specific Containers

Excludes containers by name from monitoring, even if they have the enable label set to `true`.

```text
            Argument: --disable-containers, -x
Environment Variable: WATCHTOWER_DISABLE_CONTAINERS
                Type: Comma- or space-separated string list
             Default: None
```

### Scope Filter

Monitors containers with a `com.centurylinklabs.watchtower.scope` label matching the specified value, enabling multiple Watchtower instances.

```text
            Argument: --scope
Environment Variable: WATCHTOWER_SCOPE
                Type: String
             Default: None
```

!!! Note
    Set to `none` to ignore scoped containers.
    Without this flag, Watchtower monitors all containers regardless of scope.

    For self-updates, ensure all Watchtower containers share the same `com.centurylinklabs.watchtower.scope` label to guarantee cleanup of renamed containers and old images.
    Mismatched labels may prevent detection, leaving resources running.

    See [Running Multiple Instances](../../advanced-features/running-multiple-instances/index.md).

### Label Precedence

Allows container labels (e.g., `com.centurylinklabs.watchtower.monitor-only`, `com.centurylinklabs.watchtower.no-pull`) to override corresponding flags.

```text
            Argument: --label-take-precedence
Environment Variable: WATCHTOWER_LABEL_TAKE_PRECEDENCE
                Type: Boolean
             Default: false
```

## Registry & Authentication

### REPO_USER

Sets the username for authenticating with a private registry, such as Docker Hub.

```text
            Argument: None
Environment Variable: REPO_USER
                Type: String
             Default: None
```

!!! Note
    Must be used with `REPO_PASS` to provide valid credentials.
    Suitable for simple username/password authentication.

    For Docker Hub, the registry is implicitly `https://index.docker.io/v1/`.

### REPO_PASS

Sets the password for authenticating with a private registry, such as Docker Hub.

```text
            Argument: None
Environment Variable: REPO_PASS
                Type: String
             Default: None
```

!!! Note
    Must be used with `REPO_USER`.

    Can be a password or a personal access token for registries requiring 2FA (e.g., Docker Hub).

    Use Docker secrets (e.g., `WATCHTOWER_PASS=/run/secrets/repo_pass`) or environment files to avoid exposing sensitive data in command lines.

### DOCKER_CONFIG

Specifies the directory containing the Docker configuration file (`config.json`) for registry authentication.

```text
            Argument: None
Environment Variable: DOCKER_CONFIG
                Type: String
             Default: `/`
```

!!! Note
    Useful for registries requiring complex authentication (e.g., 2FA on Docker Hub) or credential helpers (e.g., AWS ECR).

    Mount the `config.json` file to the container (e.g., `-v ~/.docker/config.json:/config.json`) and set this variable to the directory containing the file (e.g., `/`).

    Changes to the mounted file may require a symlink to ensure updates propagate.

    See [Usage](../../getting-started/usage/index.md) and [Private Registries](../private-registries/index.md).

### Skip Registry TLS Verification

Disables TLS certificate verification for registry connections, useful for self-signed certificates or insecure registries.

```text
            Argument: --registry-tls-skip
Environment Variable: WATCHTOWER_REGISTRY_TLS_SKIP
                Type: Boolean
             Default: false
```

!!! Warning
    Use cautiously, as it reduces security.
    Suitable for testing or private registries.

### Minimum Registry TLS Version

Sets the minimum TLS version for registry connections, overriding the default (TLS 1.2).

```text
            Argument: --registry-tls-min-version
Environment Variable: WATCHTOWER_REGISTRY_TLS_MIN_VERSION
     Possible Values: TLS1.0, TLS1.1, TLS1.2, TLS1.3
             Default: TLS1.2
```

!!! Warning
    Using older versions of TLS not recommended for security reasons.

### Proxy Configuration

Watchtower supports HTTP/HTTPS proxies for registry connections by respecting standard environment variables.
Set these in the Watchtower container to route requests (e.g., to Docker Hub or private registries) through a proxy.
This is useful in environments without direct internet access.

Proxy settings are read from the following variables (uppercase and lowercase variants are supported for compatibility):

```text
            Argument: None
Environment Variable: HTTP_PROXY / http_proxy
                Type: String (e.g., "http://proxy.example.com:3128")
             Default: None
```

```text
            Argument: None
Environment Variable: HTTPS_PROXY / https_proxy
                Type: String (e.g., "http://proxy.example.com:3128")
             Default: None
```

```text
            Argument: None
Environment Variable: NO_PROXY / no_proxy
                Type: Comma-separated string (e.g., "localhost,127.0.0.1,internal.example.com")
             Default: None
```

!!! Note
    Proxies may require authentication.
    Include it in the URL (e.g., `http://user:pass@proxy.example.com:3128`), but avoid exposing credentials in the command line by using Docker secrets or environment files instead.

    If your proxy uses a self-signed certificate, combine with `--registry-tls-skip` to disable TLS verification (use cautiously).

For details on how Go handles these variables, see the [net/http.ProxyFromEnvironment](https://pkg.go.dev/net/http#ProxyFromEnvironment){target="_blank" rel="noopener noreferrer"} documentation.

### Warn on HEAD Failure

Controls warnings for failed HEAD requests to registries.
`Auto` warns for registries known to support HEAD requests (e.g., docker.io) that may rate-limit.

```text
            Argument: --warn-on-head-failure
Environment Variable: WATCHTOWER_WARN_ON_HEAD_FAILURE
     Possible Values: always, auto, never
             Default: auto
```

## Docker Connection

### Docker Host

Specifies the Docker daemon socket to connect to, supporting remote hosts via TCP (e.g., `tcp://hostname:port`).

```text
            Argument: --host, -H
Environment Variable: DOCKER_HOST
                Type: String
             Default: unix:///var/run/docker.sock
```

### Docker API Version

Sets the Docker API version for client-daemon communication.

```text
            Argument: --api-version, -a
Environment Variable: DOCKER_API_VERSION
                Type: String
             Default: Autonegotiated
```

!!! Note
    Falls back to autonegotiation on failure.

!!! Warning
    Minimum supported version is Docker v1.23.

    Refer to Docker's [API version matrix](https://docs.docker.com/reference/api/engine/#api-version-matrix){target="_blank" rel="noopener noreferrer"} for compatibility.

### Enable Docker TLS Verification

Enables TLS verification for Docker socket connections.

```text
            Argument: --tlsverify
Environment Variable: DOCKER_TLS_VERIFY
                Type: Boolean
             Default: false
```

### Disable Memory Swappiness

Sets memory swappiness to `nil` for Podman compatibility with crun and cgroupv2, overriding Podman's default of `0`.

```text
            Argument: --disable-memory-swappiness
Environment Variable: WATCHTOWER_DISABLE_MEMORY_SWAPPINESS
                Type: Boolean
             Default: false
```

### CPU Copy Mode

Controls how CPU settings are copied when recreating containers, addressing Podman compatibility issues with CPU limits.
Podman handles NanoCPUs differently than Docker, which can cause container recreation failures.

```text
            Argument: --cpu-copy-mode
Environment Variable: WATCHTOWER_CPU_COPY_MODE
                Type: String
     Possible Values: auto, full, none
             Default: auto
```

!!! Note
    - **auto**: Automatically detects if running on Podman and filters NanoCPUs for compatibility. On Docker, copies all CPU settings.
    - **full**: Copies all CPU settings unchanged (original behavior).
    - **none**: Strips all CPU limits to avoid compatibility issues.

Use `auto` in mixed Docker/Podman environments.
Use `full` if running only on Docker and want to preserve all CPU limits.
Use `none` if CPU limits are causing issues and you prefer no limits on recreated containers.

#### Usage Examples

Run Watchtower with automatic CPU compatibility:

```bash
docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    --cpu-copy-mode auto
```

Force full CPU copying (Docker-only environments):

```bash
docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    --cpu-copy-mode full
```

Strip all CPU limits:

```bash
docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    --cpu-copy-mode none
```

## HTTP API & Metrics

### HTTP API Mode

Runs Watchtower in HTTP API mode, allowing updates only via HTTP requests, with support for tag-specific filtering (e.g., `image=foo/bar:1.0`).

```text
            Argument: --http-api-update
Environment Variable: WATCHTOWER_HTTP_API_UPDATE
                Type: Boolean
             Default: false
```

!!! Note
    See [HTTP API Mode](../../advanced-features/http-api/index.md) for details.

### HTTP API Token

Sets an authentication token for HTTP API requests.
Can reference a file for security.

```text
            Argument: --http-api-token
Environment Variable: WATCHTOWER_HTTP_API_TOKEN
                Type: String
             Default: None
```

### HTTP API Metrics

Enables a Prometheus metrics endpoint via HTTP.

```text
            Argument: --http-api-metrics
Environment Variable: WATCHTOWER_HTTP_API_METRICS
                Type: Boolean
             Default: false
```

!!! Note
    See [Metrics](../../advanced-features/metrics/index.md) for details.

### HTTP API Host

Sets the host to bind the HTTP API to.

```text
            Argument: --http-api-host
Environment Variable: WATCHTOWER_HTTP_API_HOST
                Type: String
             Default: empty (binds to all interfaces)
```

!!! Note
     If not specified, Watchtower listens on all interfaces on the port specified by `--http-api-port`.
     Use this option to bind to a specific host, such as `127.0.0.1` for localhost only.
     The host must be a valid IP address (IPv4 or IPv6).
     The port is set separately with `--http-api-port`.

### HTTP API Port

Sets the listening port for the HTTP API.

```text
            Argument: --http-api-port
Environment Variable: WATCHTOWER_HTTP_API_PORT
                Type: String
             Default: 8080
```

## Notifications

### Notification URL

Configures the notification service URL.
Can reference a file for sensitive values.

```text
             Argument: --notification-url
Environment Variable: WATCHTOWER_NOTIFICATION_URL
                 Type: String
              Default: None
```

### Notification Split by Container

Send separate notifications for each updated container instead of grouping them.

```text
            Argument: --notification-split-by-container
Environment Variable: WATCHTOWER_NOTIFICATION_SPLIT_BY_CONTAINER
                Type: Boolean
             Default: false
```

!!! Note
    When disabled (default), notifications are grouped for all updated containers in a single session.
    When enabled, a separate notification is sent for each container update.

### Notification Email Server Password

Sets the password for the email notification server.
Can reference a file for security.

```text
            Argument: --notification-email-server-password
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD
                Type: String
             Default: None
```

### Notification Slack Hook URL

Sets the Slack webhook URL for notifications.
Can reference a file for security.

```text
            Argument: --notification-slack-hook-url
Environment Variable: WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL
                Type: String
             Default: None
```

### Notification Microsoft Teams Hook

Sets the Microsoft Teams webhook URL for notifications.
Can reference a file for security.

```text
            Argument: --notification-msteams-hook
Environment Variable: WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK
                Type: String
             Default: None
```

### Notification Gotify Token

Sets the Gotify token for notifications.
Can reference a file for security.

```text
            Argument: --notification-gotify-token
Environment Variable: WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN
                Type: String
             Default: None
```

### Disable Startup Message

Suppresses the info-level notification sent when Watchtower starts.

```text
             Argument: --no-startup-message
Environment Variable: WATCHTOWER_NO_STARTUP_MESSAGE
                 Type: Boolean
              Default: false
```

## Git Repository Monitoring

### Enable Git Monitoring

Enables Git repository monitoring for containers with Git labels. When enabled, Watchtower monitors Git repositories for new commits and rebuilds containers automatically.

```text
             Argument: --enable-git-monitoring
Environment Variable: WATCHTOWER_GIT_ENABLE
                 Type: Boolean
              Default: false
```

!!! note
    See [Git Repository Monitoring](../../advanced-features/git-monitoring/index.md) for detailed configuration and usage.

### Git Authentication Token

Sets a default authentication token for Git repository access. Can be overridden per container using labels.

```text
             Argument: --git-auth-token
Environment Variable: WATCHTOWER_GIT_AUTH_TOKEN
                 Type: String
              Default: None
```

### Git Timeout

Sets the timeout duration for Git operations (API calls, cloning, etc.).

```text
             Argument: --git-timeout
Environment Variable: WATCHTOWER_GIT_TIMEOUT
                 Type: Duration
              Default: 30s
```

## Lifecycle & Health

### Container Stop Timeout

Sets the timeout (e.g., `30s`) before forcibly stopping a container during updates.

```text
            Argument: --stop-timeout
Environment Variable: WATCHTOWER_TIMEOUT
                Type: Duration
              Default: 30s
```

### Lifecycle UID

Sets the default user ID to run lifecycle hooks as when no container-specific UID is specified.

```text
            Argument: --lifecycle-uid
Environment Variable: WATCHTOWER_LIFECYCLE_UID
                Type: Integer
              Default: None
```

!!! Note
    Container-specific labels (`com.centurylinklabs.watchtower.lifecycle.uid`) take precedence over this global setting.

    See [Lifecycle Hooks](../../advanced-features/lifecycle-hooks/index.md).

### Lifecycle GID

Sets the default group ID to run lifecycle hooks as when no container-specific GID is specified.

```text
            Argument: --lifecycle-gid
Environment Variable: WATCHTOWER_LIFECYCLE_GID
                Type: Integer
              Default: None
```

!!! Note
    Container-specific labels (`com.centurylinklabs.watchtower.lifecycle.gid`) take precedence over this global setting.

    See [Lifecycle Hooks](../../advanced-features/lifecycle-hooks/index.md).

### Health Check

Returns a success exit code for Docker `HEALTHCHECK`, verifying another process is running in the container.

```text
            Argument: --health-check
Environment Variable: None
                Type: N/A
             Default: N/A
```

!!! Note
    Intended solely for Docker `HEALTHCHECK`.
    Do not use on the main command line.

## Output & Compatibility

### Programmatic Output (Porcelain)

Outputs session results in a machine-readable format (version specified by `VERSION`).

```text
            Argument: --porcelain, -P
Environment Variable: WATCHTOWER_PORCELAIN
     Possible Values: v1
             Default: None
```

!!! Note
    Equivalent to:
    ```text
    --notification-url logger://
    --notification-log-stdout
    --notification-report
    --notification-template porcelain.VERSION.summary-no-log
    ```
