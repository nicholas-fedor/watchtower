# Container Selection

## Include Stopped Containers

Includes created and exited containers in monitoring and updates.

```text
            Argument: --include-stopped, -S
Environment Variable: WATCHTOWER_INCLUDE_STOPPED
                Type: Boolean
             Default: false
```

## Revive Stopped Containers

Restarts stopped containers after their images are updated.

```text
            Argument: --revive-stopped
Environment Variable: WATCHTOWER_REVIVE_STOPPED
                Type: Boolean
             Default: false
```

!!! Note
    Requires `--include-stopped`.

## Include Restarting Containers

Includes containers in the restarting state for monitoring and updates.

```text
            Argument: --include-restarting
Environment Variable: WATCHTOWER_INCLUDE_RESTARTING
                Type: Boolean
             Default: false
```

## Enable Label Filter

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

## Disable Specific Containers

Excludes containers by container name from monitoring, even if they have the enable label set to `true`.

```text
            Argument: --disable-containers, -x
Environment Variable: WATCHTOWER_DISABLE_CONTAINERS
                Type: Comma- or space-separated string list
             Default: None
```

!!! Note
    Regex patterns are supported. See [Regex Pattern Matching](../../getting-started/container-selection/index.md#regex_pattern_matching) for details.

## Monitor Specific Images

Restricts monitoring to containers whose image name matches one of the supplied image name patterns, even if other selection criteria would include them.

```text
            Argument: --monitor-image-names
Environment Variable: WATCHTOWER_MONITOR_IMAGE_NAMES
                Type: Comma or space-separated string list
             Default: None
```

!!! Note
    Image name patterns include the tag (for example `nginx:latest`).
    Regex patterns are supported and anchored to the **full** image name.
    See [Regex Pattern Matching](../../getting-started/container-selection/index.md#regex_pattern_matching)
    for details.

## Skip Specific Images

Excludes containers by image name pattern from monitoring, even if they have the enable label set to `true`.

```text
            Argument: --skip-image-names
Environment Variable: WATCHTOWER_SKIP_IMAGE_NAMES
                Type: Comma or space-separated string list
             Default: None
```

!!! Note
    Image name patterns include the tag (for example `nginx:latest`).
    Regex patterns are supported and anchored to the **full** image name.
    See [Regex Pattern Matching](../../getting-started/container-selection/index.md#regex_pattern_matching)
    for details.

## Scope Filter

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

## Label Precedence

Allows container labels (e.g., `com.centurylinklabs.watchtower.monitor-only`, `com.centurylinklabs.watchtower.no-pull`) to override corresponding flags.

```text
            Argument: --label-take-precedence
Environment Variable: WATCHTOWER_LABEL_TAKE_PRECEDENCE
                Type: Boolean
             Default: false
```

## Use Docker Compose Depends-On

Enables or disables processing of the Docker Compose [`depends_on`](https://docs.docker.com/reference/compose-file/services/#depends_on){target="_blank" rel="noopener noreferrer"} configuration for determining container update order.

```text
            Argument: --use-compose-depends-on
Environment Variable: WATCHTOWER_USE_COMPOSE_DEPENDS_ON
                Type: Boolean
             Default: true
```

- By default, Watchtower automatically detects and respects Docker Compose service dependencies.
- When this feature is disabled, only the Watchtower `depends-on` label, Docker links, and network mode are used.

!!! Note
    Disabling this is useful when you want to prevent Watchtower from automatically using Docker Compose dependencies but still use explicit Watchtower labels or Docker links for ordering.
    For more information on Watchtower's handling of linked containers, please reference the [Linked Containers documentation](../../advanced-features/linked-containers/index.md).

!!! Warning
    Rolling restarts are not supported when any container has linked dependencies (including Docker Compose `depends_on`, Watchtower `depends-on` labels, Docker links, or network mode dependencies).
    When [`rolling-restart`](../../configuration/update-behavior/index.md#rolling_restart) is enabled, the [`use-compose-depends-on`](../../configuration/container-selection/index.md#use_docker_compose_depends-on) configuration option controls whether Docker Compose `depends_on` labels are included in the dependency validation check.
