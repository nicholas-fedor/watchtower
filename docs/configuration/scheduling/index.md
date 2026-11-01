# Scheduling

## Schedule

Defines when and how often Watchtower checks for new images using a [cron expression](https://pkg.go.dev/github.com/robfig/cron/v3#hdr-CRON_Expression_Format){target="_blank" rel="noopener noreferrer"}. The schedule takes one of these forms:

- 5 fields: minute, hour, day of month, month and day of week.
- 6 fields, with a seconds field before the minute.
- A [predefined schedule](https://pkg.go.dev/github.com/robfig/cron/v3#hdr-Predefined_schedules){target="_blank" rel="noopener noreferrer"}, such as `@daily`.
- An [interval](https://pkg.go.dev/github.com/robfig/cron/v3#hdr-Intervals){target="_blank" rel="noopener noreferrer"}, such as `@every 6h`, counted from when Watchtower starts.

Example: `--schedule "0 4 * * *"` and `--schedule "0 0 4 * * *"` both run daily at 4 AM.

```text
            Argument: --schedule, -s
Environment Variable: WATCHTOWER_SCHEDULE
                Type: String
             Default: None
```

!!! Note
    - Cannot be used with the [`interval`](#interval) configuration option.
    - Requires a time zone set via `TZ` or a mounted `/etc/localtime` file. See [Time Zone](#time_zone).
    - Shared registry buckets, such as GHCR's for `lscr.io/linuxserver/*` images, are empty for the first minute or two of every hour. Pick a minute away from `:00`, for example `0 17 3 * * *`. See [GitHub Container Registry](../../advanced-features/image-cooldown/index.md#github_container_registry_ghcrio).

## Interval

Sets the interval (in seconds) for polling new images. Watchtower exits at startup if the value is zero or negative.

```text
            Argument: --interval, -i
Environment Variable: WATCHTOWER_POLL_INTERVAL
                Type: Integer
             Default: 86400 (24 hours)
```

!!! Note
    Cannot be used with `--schedule`.
    Overrides cron-based scheduling.

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
