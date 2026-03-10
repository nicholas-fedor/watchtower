# Container Selection

By default, Watchtower will watch all containers. However, sometimes only some containers should be updated.

There are two options:

- **Fully exclude**: You can choose to exclude containers entirely from being watched by Watchtower.
- **Monitor only**: In this mode, Watchtower checks for container updates, sends notifications and invokes the [pre-check/post-check hooks](../../advanced-features/lifecycle-hooks/index.md) on the containers but does **not** perform the update.

## Full Exclude

If you need to exclude some containers, set the _com.centurylinklabs.watchtower.enable_ label to `false`.
For clarity this should be set **on the container(s)** you wish to be ignored, this is not set on Watchtower.
<!-- markdownlint-disable -->
=== "dockerfile"

    ```docker
    LABEL com.centurylinklabs.watchtower.enable="false"
    ```
=== "docker run"

    ```bash
    docker run -d --label=com.centurylinklabs.watchtower.enable=false someimage
    ```

=== "docker-compose"

    ``` yaml
    version: "3"
    services:
      someimage:
        container_name: someimage
        labels:
          - "com.centurylinklabs.watchtower.enable=false"
    ```
<!-- markdownlint-restore -->
If instead you want to [only include containers with the enable label](../arguments/index.md#enable_label_filter), pass the `--label-enable` flag or the `WATCHTOWER_LABEL_ENABLE` environment variable on startup for Watchtower and set the _com.centurylinklabs.watchtower.enable_ label with a value of `true` on the containers you want to watch.
<!-- markdownlint-disable -->
=== "dockerfile"

    ```docker
    LABEL com.centurylinklabs.watchtower.enable="true"
    ```
=== "docker run"

    ```bash
    docker run -d --label=com.centurylinklabs.watchtower.enable=true someimage
    ```

=== "docker-compose"

    ``` yaml
    version: "3"
    services:
      someimage:
        container_name: someimage
        labels:
          - "com.centurylinklabs.watchtower.enable=true"
    ```
<!-- markdownlint-restore -->
If you wish to create a monitoring scope, you will need to [run multiple instances and set a scope for each of them](../../advanced-features/running-multiple-instances/index.md).

Watchtower filters running containers by testing them against each configured criteria.
A container is monitored if all criteria are met.

For example:

- If a container's name is on the monitoring name list (not empty `--name` argument), but it is not enabled (_centurylinklabs.watchtower.enable=false_), then it won't be monitored.
- If a container's name is not on the monitoring name list (not empty `--name` argument), even if it is enabled (_centurylinklabs.watchtower.enable=true_ and `--label-enable` flag is set), then it won't be monitored.

## Monitor Only

Individual containers can be marked to only be monitored and will not be updated by Watchtower.

To do so, set the _com.centurylinklabs.watchtower.monitor-only_ label to `true` on that container:

```docker
LABEL com.centurylinklabs.watchtower.monitor-only="true"
```

Or, it can be specified as part of the `docker run` command line:

```bash
docker run -d --label=com.centurylinklabs.watchtower.monitor-only=true someimage
```

When the label is specified on a container, Watchtower treats that container exactly as if [`WATCHTOWER_MONITOR_ONLY`](../arguments/index.md#monitor_only) was set, but the effect is limited to the individual container.

## Regex Pattern Matching

Both container inclusion (positional arguments) and exclusion  ([`--disable-containers`/`WATCHTOWER_DISABLE_CONTAINERS`](../arguments/index.md#disable_specific_containers)) support regular expression patterns for matching container names.

!!! Note "Patterns are anchored to match the **full container name**"
    Use `.*` (period + asterisk) for wildcards instead of just an `*` (asterisk)

### Syntax

- Patterns use [Go regex syntax](https://pkg.go.dev/regexp/syntax)
- Patterns are anchored to match the **entire** container name
- Invalid regex patterns fall back to literal string matching

### Examples

| Pattern | Matches |
|---------|---------|
| `container.*` | "container1", "container-abc", "mycontainer" |
| `.*-dev` | "web-dev", "api-dev", "db-dev" |
| `.*` | Any container name |
| `nginx|redis` | Either "nginx" or "redis" |
| `^web-.*$` | All containers starting with "web-" |

- Exclude all containers starting with a specific prefix:

```bash
docker run -d -e WATCHTOWER_DISABLE_CONTAINERS="web-.*" nickfedor/watchtower
```

- Include only containers matching a pattern:

```bash
watchtower "db-.*" "cache-.*"
```
