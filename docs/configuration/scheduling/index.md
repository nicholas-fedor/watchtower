# Scheduling

## Schedule

Defines when and how often Watchtower checks for new images using a 6-field [Cron expression](https://pkg.go.dev/github.com/robfig/cron@v1.2.0?tab=doc#hdr-CRON_Expression_Format){target="_blank" rel="noopener noreferrer"}.

Example: `--schedule "0 0 4 * * *"` runs daily at 4 AM.

```text
            Argument: --schedule, -s
Environment Variable: WATCHTOWER_SCHEDULE
                Type: String
             Default: None
```

!!! Note
    - Cannot be used with the [`interval`](#interval) configuration option.
    - Requires a time zone set via `TZ` or a mounted `/etc/localtime` file. See [Time Zone](#time_zone).

## Interval

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

## Per-Container Schedule

Individual containers can override the default schedule (set via `--schedule`
or `--interval`) using the `com.centurylinklabs.watchtower.schedule` label.
The label accepts the same 6-field cron expressions and descriptors (e.g.,
`@hourly`, `@every 30m`) as `--schedule`.

Containers carrying the label are checked on their own schedule instead of the
default one. If the label value is not a valid cron expression, a warning is
logged and the container keeps following the default schedule. Containers
created after startup are picked up automatically when their labels are
rescanned.

<!-- markdownlint-disable -->
=== "Docker Compose"
    ```yaml
    services:
        app:
            image: someimage
            labels:
                - "com.centurylinklabs.watchtower.schedule=0 0 3 * * *"
    ```
=== "Docker CLI"
    ```bash
    docker run -d \
        --label=com.centurylinklabs.watchtower.schedule="0 0 3 * * *" \
        someimage
    ```
<!-- markdownlint-enable -->

!!! Note
    The label only affects scheduled runs. Manual update triggers (for
    example the [`update`](../../http-api/endpoints/update/index.md) HTTP API
    endpoint) still include the container regardless of its schedule label.

## Run Once

Triggers a single update attempt for specified containers and exits immediately.

```text
            Argument: --run-once, -R
Environment Variable: WATCHTOWER_RUN_ONCE
                Type: Boolean
             Default: false
```

!!! Note "Watchtower automatically sets its own restart policy to "no" in run-once mode to prevent unwanted restarts."

## Time Zone

Sets the time zone for Watchtower's logs and the `--schedule` flag's cron expressions.

```text
            Argument: None
Environment Variable: TZ
                Type: String
             Default: UTC
```

- To specify a time zone, use a value from the [TZ Database](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones){target="_blank" rel="noopener noreferrer"} (e.g., `Europe/Rome`).
- Alternatively, mount the host's `/etc/localtime` file using `-v /etc/localtime:/etc/localtime:ro`.

## Update on Start

Performs an update check on startup.
If a schedule is configured (via --schedule or --interval), then Watchtower continues with periodic updates.

```text
            Argument: --update-on-start
Environment Variable: WATCHTOWER_UPDATE_ON_START
                Type: Boolean
             Default: false
```

!!! Note
    If used with [`run-once`](#run_once), a warning is logged and [`run-once`](#run_once) takes precedence.

## HTTP API Periodic Polls

Enables periodic updates when the HTTP API update endpoint is active.

```text
            Argument: --http-api-periodic-polls
Environment Variable: WATCHTOWER_HTTP_API_PERIODIC_POLLS
                Type: Boolean
             Default: false
```

!!! Note
    - Requires the [`update`](../../http-api/endpoints/update/index.md) endpoint to be enabled via the [`http-api-endpoints`](../http-api/index.md#http_api_endpoints) configuration option.
    - The deprecated [`http-api-update`](../http-api/index.md#http_api_update) configuration option still works until Watchtower v2. (See [Deprecated Configuration Options](../http-api/index.md#deprecated_configuration_options)).
