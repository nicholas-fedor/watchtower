By default, Watchtower monitors all containers running on the Docker daemon it connects to (typically the local daemon, configurable via the `--host` flag). To limit monitoring to specific containers, provide their names as arguments when starting Watchtower.

```bash
$ docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    nginx redis
```

In this example, Watchtower monitors only the "nginx" and "redis" containers, ignoring others. To run a single update attempt and exit, use the `--run-once` flag with the `--rm` option to remove the Watchtower container afterward.

```bash
$ docker run --rm \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    --run-once \
    nginx redis
```

This command triggers an update attempt for "nginx" and "redis" containers, displays debug output, and removes the Watchtower container upon completion. Without arguments, Watchtower monitors all running containers.

## Secrets/Files

Certain flags support referencing a file, using its contents as the value, to securely handle sensitive data like passwords or tokens, avoiding exposure in configuration files or command lines.

| Flag                            | Environment Variable                             |
|---------------------------------|-------------------------------------------------|
| `--notification-url`            | `WATCHTOWER_NOTIFICATION_URL`                   |
| `--notification-email-server-password` | `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD` |
| `--notification-slack-hook-url` | `WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL`        |
| `--notification-msteams-hook`   | `WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK`          |
| `--notification-gotify-token`   | `WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN`          |
| `--http-api-token`              | `WATCHTOWER_HTTP_API_TOKEN`                     |

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

Sets the time zone for Watchtower's logs and the `--schedule` flag's cron expressions. Without this setting, Watchtower defaults to UTC. To specify a time zone, use a value from the [TZ Database](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) (e.g., `Europe/Rome`). Alternatively, mount the host's `/etc/localtime` file using `-v /etc/localtime:/etc/localtime:ro`.

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

!!! note
    Equivalent to `--log-level debug`. See [Maximum Log Level](#maximum-log-level). As an argument, does not accept a value (e.g., `--debug true` is invalid).

### Trace

Enables trace mode with highly verbose logging, including sensitive information like credentials.

```text
            Argument: --trace
Environment Variable: WATCHTOWER_TRACE
                Type: Boolean
             Default: false
```

!!! note
    Equivalent to `--log-level trace`. See [Maximum Log Level](#maximum-log-level). As an argument, does not accept a value (e.g., `--trace true` is invalid). Use with caution due to credential exposure.

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
```

!!! note
    Enables debug output during execution, suitable for interactive use. Use with `--rm` to remove the Watchtower container after completion.

## Scheduling & Polling

### Schedule

Defines when and how often Watchtower checks for new images using a 6-field [Cron expression](https://pkg.go.dev/github.com/robfig/cron@v1.2.0?tab=doc#hdr-CRON_Expression_Format). Example: `--schedule "0 0 4 * * *"` runs daily at 4 AM.

```text
            Argument: --schedule, -s
Environment Variable: WATCHTOWER_SCHEDULE
                Type: String
             Default: None
```

!!! note
    Cannot be used with `--interval`. Requires a time zone set via `TZ` or a mounted `/etc/localtime` file (see [Time Zone](#time-zone)).

### Poll Interval

Sets the interval (in seconds) for polling new images.

```text
            Argument: --interval, -i
Environment Variable: WATCHTOWER_POLL_INTERVAL
                Type: Integer
             Default: 86400 (24 hours)
```

!!! note
    Cannot be used with `--schedule`. Overrides cron-based scheduling.

### HTTP API Periodic Polls

Enables periodic updates when HTTP API mode is active, allowing both API-triggered and scheduled updates.

```text
            Argument: --http-api-periodic-polls
Environment Variable: WATCHTOWER_HTTP_API_PERIODIC_POLLS
                Type: Boolean
             Default: false
```

!!! note
    Requires `--http-api-update`. See [HTTP API Mode](#http-api-mode).

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

!!! note
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

Prevents restarting containers after updating, useful when an external system (e.g., systemd) manages container lifecycle.

```text
            Argument: --no-restart
Environment Variable: WATCHTOWER_NO_RESTART
                Type: Boolean
             Default: false
```

!!! warning
    Combining `--no-restart` with `--cleanup` during Watchtower self-update may leave a renamed Watchtower container running without starting a new one, preventing cleanup of the old image. Use cautiously for self-updating Watchtower instances, and consider external lifecycle management (e.g., Docker Compose) to restart containers manually.

### Rolling Restart

Restarts containers one at a time to minimize downtime, ideal for zero-downtime deployments with lifecycle hooks.

```text
            Argument: --rolling-restart
Environment Variable: WATCHTOWER_ROLLING_RESTART
                Type: Boolean
             Default: false
```

!!! note
    When combined with `--cleanup`, image cleanup is deferred until all containers are updated, which may temporarily increase disk usage for large numbers of containers (>50). This is typically negligible for homelab setups but monitor disk space on resource-constrained hosts.

### Cleanup Old Images

Removes old images after updating containers to free disk space.

```text
            Argument: --cleanup
Environment Variable: WATCHTOWER_CLEANUP
                Type: Boolean
             Default: false
```

!!! note
    During Watchtower self-updates, cleanup is deferred to the new container to prevent premature image deletion. Ensure `--no-restart` is not used with `--cleanup` to avoid incomplete updates.

### Remove Anonymous Volumes

Deletes anonymous volumes when updating containers. Named volumes remain unaffected.

```text
            Argument: --remove-volumes
Environment Variable: WATCHTOWER_REMOVE_VOLUMES
                Type: Boolean
             Default: false
```

### Monitor Only

Monitors for new images, sends notifications, and runs lifecycle hooks without updating containers.

```text
            Argument: --monitor-only
Environment Variable: WATCHTOWER_MONITOR_ONLY
                Type: Boolean
             Default: false
```

!!! note
    Images may still be pulled due to Docker API limitations for digest comparison. Can be set per container via the `com.centurylinklabs.watchtower.monitor-only` label. See [Label Precedence](#label-precedence).

### Disable Image Pulling

Prevents pulling new images from registries, monitoring only local image cache changes. Useful for locally built images.

```text
            Argument: --no-pull
Environment Variable: WATCHTOWER_NO_PULL
                Type: Boolean
             Default: false
```

!!! note
    Can be set per container via the `com.centurylinklabs.watchtower.no-pull` label. See [Label Precedence](#label-precedence).

### Enable Label Filter

Restricts monitoring to containers with the `com.centurylinklabs.watchtower.enable` label set to `true` when the `--label-enable` flag is specified. Without `--label-enable`, containers with this label set to `false` are excluded, while others are monitored by default.

```text
            Argument: --label-enable
Environment Variable: WATCHTOWER_LABEL_ENABLE
                Type: Boolean
             Default: false
```

!!! note
    When `--label-enable` is unset, containers without the `com.centurylinklabs.watchtower.enable` label or with it set to `true` are monitored, and those with `false` are excluded. When `--label-enable` is set, only containers with `true` are monitored, ignoring those with `false` or no label.

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

!!! note
    Set to `none` to ignore scoped containers. Without this flag, Watchtower monitors all containers regardless of scope. For self-updates, ensure all Watchtower containers share the same `com.centurylinklabs.watchtower.scope` label to guarantee cleanup of renamed containers and old images. Mismatched labels may prevent detection, leaving resources running. See [Running Multiple Instances](https://nicholas-fedor.github.io/watchtower/running-multiple-instances).

### Label Precedence

Allows container labels (e.g., `com.centurylinklabs.watchtower.monitor-only`, `com.centurylinklabs.watchtower.no-pull`) to override corresponding flags.

```text
            Argument: --label-take-precedence
Environment Variable: WATCHTOWER_LABEL_TAKE_PRECEDENCE
                Type: Boolean
             Default: false
```

## Registry & Authentication

### Skip Registry TLS Verification

Disables TLS certificate verification for registry connections, useful for self-signed certificates or insecure registries.

```text
            Argument: --registry-tls-skip
Environment Variable: WATCHTOWER_REGISTRY_TLS_SKIP
                Type: Boolean
             Default: false
```

!!! note
    Use cautiously, as it reduces security. Suitable for testing or private registries.

### Minimum Registry TLS Version

Sets the minimum TLS version for registry connections, overriding the default (TLS 1.2).

```text
            Argument: --registry-tls-min-version
Environment Variable: WATCHTOWER_REGISTRY_TLS_MIN_VERSION
     Possible Values: TLS1.0, TLS1.1, TLS1.2, TLS1.3
             Default: TLS1.2
```

### Warn on HEAD Failure

Controls warnings for failed HEAD requests to registries. `Auto` warns for registries known to support HEAD requests (e.g., docker.io) that may rate-limit.

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

Sets the Docker API version for client-daemon communication. Defaults to autonegotiation.

```text
            Argument: --api-version, -a
Environment Variable: DOCKER_API_VERSION
                Type: String
             Default: Autonegotiated
```

!!! note
    Minimum supported version is 1.24. Refer to Docker's [API version matrix](https://docs.docker.com/reference/api/engine/#api-version-matrix) for compatibility.

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

## HTTP API & Metrics

### HTTP API Mode

Runs Watchtower in HTTP API mode, allowing updates only via HTTP requests, with support for tag-specific filtering (e.g., `image=foo/bar:1.0`).

```text
            Argument: --http-api-update
Environment Variable: WATCHTOWER_HTTP_API_UPDATE
                Type: Boolean
             Default: false
```

!!! note
    See [HTTP API Mode](https://nicholas-fedor.github.io/watchtower/http-api-mode) for details.

### HTTP API Token

Sets an authentication token for HTTP API requests. Can reference a file for security.

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

!!! note
    See [Metrics](https://nicholas-fedor.github.io/watchtower/metrics) for details.

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

Configures the notification service URL. Can reference a file for sensitive values.

```text
            Argument: --notification-url
Environment Variable: WATCHTOWER_NOTIFICATION_URL
                Type: String
             Default: None
```

### Notification Email Server Password

Sets the password for the email notification server. Can reference a file for security.

```text
            Argument: --notification-email-server-password
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD
                Type: String
             Default: None
```

### Notification Slack Hook URL

Sets the Slack webhook URL for notifications. Can reference a file for security.

```text
            Argument: --notification-slack-hook-url
Environment Variable: WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL
                Type: String
             Default: None
```

### Notification Microsoft Teams Hook

Sets the Microsoft Teams webhook URL for notifications. Can reference a file for security.

```text
            Argument: --notification-msteams-hook
Environment Variable: WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK
                Type: String
             Default: None
```

### Notification Gotify Token

Sets the Gotify token for notifications. Can reference a file for security.

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

## Lifecycle & Health

### Container Stop Timeout

Sets the timeout (e.g., `30s`) before forcibly stopping a container during updates.

```text
            Argument: --stop-timeout
Environment Variable: WATCHTOWER_TIMEOUT
                Type: Duration
             Default: 30s
```

### Health Check

Returns a success exit code for Docker `HEALTHCHECK`, verifying another process is running in the container.

```text
            Argument: --health-check
Environment Variable: None
                Type: N/A
             Default: N/A
```

!!! note
    Intended solely for Docker `HEALTHCHECK`. Do not use on the main command line.

## Output & Compatibility

### Programmatic Output (Porcelain)

Outputs session results in a machine-readable format (version specified by `VERSION`).

```text
            Argument: --porcelain, -P
Environment Variable: WATCHTOWER_PORCELAIN
     Possible Values: v1
             Default: None
```

!!! note
    Equivalent to:
    ```text
    --notification-url logger://
    --notification-log-stdout
    --notification-report
    --notification-template porcelain.VERSION.summary-no-log
    ```
