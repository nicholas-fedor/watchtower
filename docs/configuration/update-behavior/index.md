# Update Behavior

## Disable Container Restart

Stops and removes the old containers and creates new ones with the updated image, but does not start the new containers.
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

## Rolling Restart

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

!!! Warning "This functionality is currently not supported when used in combination with linked-containers."
     This limitation exists because linked-containers require coordinated updates across dependency chains, which conflicts with the incremental nature of rolling restarts.

## Cleanup Old Images

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

## Remove Anonymous Volumes

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

## Container Stop Timeout

Sets the timeout (e.g., `30s`) before forcibly stopping a container during updates.

```text
            Argument: --stop-timeout
Environment Variable: WATCHTOWER_TIMEOUT
                Type: Duration (e.g., 30s, 1m, 5m)
              Default: 30s
```

!!! Note
    Bare numeric values (e.g., `60` or `1.5`) without a time unit are interpreted as seconds.
    Using a unit suffix (`s`, `m`, etc.) is recommended and required for other time units.

## Monitor Only

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

    See [Label Precedence](../container-selection/index.md#label_precedence).

## Disable Image Pulling

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

    See [Label Precedence](../container-selection/index.md#label_precedence).

## Ephemeral Self-Update

Uses a short-lived orchestrator container to perform Watchtower self-updates instead of the default rename-based approach.

```text
            Argument: --ephemeral-self-update
Environment Variable: WATCHTOWER_EPHEMERAL_SELF_UPDATE
                Type: Boolean
             Default: false
```

!!! Warning "This is an experimental feature."

!!! Note
    The ephemeral self-update mechanism is only active when Watchtower is running in normal daemon mode.
    When Watchtower is started with the [`run-once`](../scheduling/index.md#run_once) configuration option, this flag is ignored because the process exits immediately after the initial update pass and there is no continuously running instance to replace.
    See [Advanced Features - Ephemeral Self-Updates](../../advanced-features/ephemeral-self-updates/index.md) for details on how this mechanism works.
