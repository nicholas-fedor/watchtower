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

## Container Schedules

Lets a container set its own update schedule with the `com.centurylinklabs.watchtower.schedule` label, in place of the global [schedule](#schedule) or [interval](#interval).
The label takes the same cron syntax as `--schedule`, including the `@every` and `@hourly` descriptors.

```text
            Argument: --container-schedules
Environment Variable: WATCHTOWER_CONTAINER_SCHEDULES
                Type: Boolean
             Default: false
```

```yaml
services:
  game:
    image: example/game:latest
    labels:
      - com.centurylinklabs.watchtower.enable=true
      # checked every 10 minutes; everything else on the global schedule
      - com.centurylinklabs.watchtower.schedule=@every 10m
```

- A container with a schedule label is checked for updates on its own schedule, and the global schedule checks the containers without one. Schedules that fire at the same moment, the global one included, share one update run.
- Every run covers the containers Watchtower manages, as a global run does, but checks only the containers whose schedule fired. Dependencies work as in a global run: when a container is updated, the containers that depend on it restart with it, in dependency order, whatever their schedule. They are not checked for updates until their own schedule fires.
- Watchtower reads the schedule labels at startup and after each update run, from the container list the run uses anyway. A new or changed label takes effect after the next run of any schedule.
- A label that does not parse is ignored with a warning, and the container follows the global schedule.
- The other selection settings still apply: a labeled container in another [scope](../container-selection/index.md#scope_filter), or one not enabled when [label-enable](../container-selection/index.md#enable_label_filter) is set, is not updated.
- Leaving out both [schedule](#schedule) and [interval](#interval) keeps the default global schedule, which checks the containers without a label once a day. To update only the containers with a schedule label, pass `--schedule ""` on the command line (an empty `WATCHTOWER_SCHEDULE` is ignored). [Update on start](#update_on_start) checks the containers on the global schedule.
- With the [HTTP API](../http-api/index.md)'s update endpoint, scheduled updates (container schedules included) run only when [HTTP API periodic polls](#http_api_periodic_polls) are enabled. An update request covers every container it selects, whatever its schedule.

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
