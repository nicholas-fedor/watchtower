# Settings

## Overview

The `/v1/settings` endpoint reads and changes the settings that can be adjusted while Watchtower is running, without editing flags or environment variables and restarting.
Include `settings` in [`http-api-endpoints`](../../../configuration/http-api/index.md#http_api_endpoints) to enable it. The [web UI](../ui/index.md) uses it for its Settings page.

Flags and environment variables still provide the starting values. A change made through this endpoint overrides the starting value until it is reset.

| **Setting**       | **Type** | **Starts from**                                                                    | **Effect**                                                              |
|:------------------|:--------:|:-----------------------------------------------------------------------------------|:------------------------------------------------------------------------|
| `monitor_only`    | Boolean  | [`monitor-only`](../../../configuration/update-behavior/index.md#monitor_only)               | Report available updates without applying them                          |
| `cleanup`         | Boolean  | [`cleanup`](../../../configuration/update-behavior/index.md#cleanup_old_images)                    | Remove the previous image after a container is updated                  |
| `rolling_restart` | Boolean  | [`rolling-restart`](../../../configuration/update-behavior/index.md#rolling_restart)            | Restart containers one at a time                                        |
| `schedule`        | String   | [`schedule`](../../../configuration/scheduling/index.md) or the poll interval      | Cron expression for scheduled updates. An empty string turns them off   |

Everything else, including the API token, the listen address, the Docker connection, and registry credentials, can only be set with flags and environment variables.

## Keeping Changes

Changes are saved to `settings.json` in the [data directory](../../../configuration/http-api/index.md#data_directory), which defaults to `/data`. Mount a volume there to keep them across restarts:

```yaml
services:
  watchtower:
    image: nickfedor/watchtower
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./watchtower-data:/data
```

Without a writable data directory, changes still take effect but are lost when Watchtower restarts. The `saved` field of the response reports which applies.

The same directory holds `history.json`, the scan history returned by [`/v1/history`](../history/index.md).

!!! Note
    Saved settings are ignored for `--run-once`, which always follows its flags.

## Reading Settings

```bash
curl -H "Authorization: Bearer <token>" http://localhost:8080/v1/settings
```

```json
{
    "settings": {
        "monitor_only": {"value": false, "default": false, "changed": false},
        "cleanup": {"value": true, "default": false, "changed": true},
        "rolling_restart": {"value": false, "default": false, "changed": false},
        "schedule": {"value": "@every 6h", "default": "@every 86400s", "changed": true}
    },
    "saved": true,
    "schedule_active": true,
    "timestamp": "2025-01-20T11:35:00Z",
    "api_version": "v1"
}
```

- `value` is the value in force, `default` is the starting value from flags and environment variables, and `changed` is `true` when the value was set through this endpoint.
- `saved` is `true` when changes are written to the data directory.
- `schedule_active` is `false` when the [`update`](../update/index.md) endpoint is enabled without [`http-api-periodic-polls`](../../../configuration/scheduling/index.md#http_api_periodic_polls). Watchtower then only updates on request, so the schedule has no effect.

## Changing Settings

Send a `PATCH` request naming only the settings to change. Send `null` to return a setting to its starting value.

```bash
curl -X PATCH \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"cleanup": true, "schedule": "0 0 4 * * *"}' \
  http://localhost:8080/v1/settings
```

The response has the same shape as a read. Changes apply to the next update, and a new schedule replaces the old one straight away. With an `@every` schedule, the first run comes one interval after the change.

A request is applied in full or not at all. It is rejected with `400` and an `error` message when:

- it names an unknown setting or gives a value of the wrong type,
- `schedule` is not a valid cron expression,
- it would turn on both `rolling_restart` and `monitor_only`, or
- it turns on `rolling_restart` while watched containers have dependencies that rule it out.

## HTTP Status Codes

| **Status** | **Meaning**                                  |
|:----------:|:---------------------------------------------|
|   `200`    | Settings returned, or the change was applied |
|   `400`    | The change was rejected                      |
|   `401`    | Missing or invalid API token                 |

## Security

Anyone holding the API token can turn `monitor_only` off or change when updates run. Treat this endpoint like [`update`](../update/index.md): leave `settings` out of the list if the token is shared with read-only consumers.
