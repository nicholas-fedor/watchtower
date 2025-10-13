# HTTP API

## Overview

Watchtower has an [optional](../../configuration/arguments/index.md#http_api_mode) HTTP API server.

!!! Caution
    This is a relatively simple API with significant security implications.

## Endpoints

|            **Name**            | **Endpoint**  |          **Parameters**           |                                    **Description**                                    |
|:------------------------------:|:-------------:|:---------------------------------:|:-------------------------------------------------------------------------------------:|
|   [Update](#http_api_update)   | `/v1/update`  | [`image`](#image_parameter_usage) | Triggers container updates and returns JSON results of the operation                 |
| [Metrics](../metrics/index.md) | `/v1/metrics` |                                   | Exposes Prometheus-compatible metrics for monitoring and alerting                     |

### HTTP API Update

To enable this mode, use the `--http-api-update` CLI argument or the `WATCHTOWER_HTTP_API_UPDATE` environment variable.

#### Response Format

The `/v1/update` endpoint returns a JSON response containing the results of the update operation:

```json
{
  "summary": {
    "scanned": 8,
    "updated": 0,
    "failed": 0
  },
  "timing": {
    "duration_ms": 1250,
    "duration": "1.25s"
  },
  "timestamp": "2025-01-20T11:30:45Z",
  "api_version": "v1"
}
```

**Summary Section:**

- `scanned`: Number of containers that were scanned for updates
- `updated`: Number of containers that were successfully updated
- `failed`: Number of containers where the update failed

**Timing Section:**

- `duration_ms`: Execution time in milliseconds
- `duration`: Human-readable execution time

**Metadata:**

- `timestamp`: UTC timestamp when the response was generated (RFC3339 format)
- `api_version`: API version identifier

#### Requirements

##### Authentication

Watchtower uses token-based, header authentication for the HTTP API.

This should be set using the [HTTP API Token](../../configuration/arguments/index.md#http_api_token) configuration option.

All requests to the `/v1/update` endpoint will require an `Authorization: Bearer <token>` header with the predefined HTTP API token value.

##### Address and Port Configuration

Watchtower defaults to listening on all interfaces on port 8080.
The port can be changed using the [HTTP API Port](../../configuration/arguments/index.md#http_api_port) configuration option.
To bind to a specific host, use the [HTTP API Host](../../configuration/arguments/index.md#http_api_host) configuration option.
The host must be a valid IP address (IPv4 or IPv6).

Alternatively, if Watchtower is being run via a Docker container, then the `host:container` port mapping can be updated accordingly (e.g. `8080:8080` -> `9000:8080`).

###### Examples

- Listen on all interfaces on port 8080 (default):

  ```bash
  --http-api-port=8080
  ```

- Listen on localhost only on port 8080:

  ```bash
  --http-api-host=127.0.0.1 --http-api-port=8080
  ```

- Listen on a specific IP and port:

  ```bash
  --http-api-host=192.168.1.100 --http-api-port=9090
  ```

#### Image Parameter Usage

Watchtower supports using the `image` URL query parameter to filter updates for only certain images.

##### No Image Filtering

The following `curl` command would trigger an update of all container images monitored by Watchtower:

```bash
curl -H "Authorization: Bearer mytoken" localhost:8080/v1/update
```

##### Image Filtering with Tags

You can specify image tags to target containers running a specific version (e.g., `foo/bar:1.0`).

For example, to update only containers using `foo/bar:1.0` and `foo/baz:latest`:

```bash
curl -H "Authorization: Bearer mytoken" localhost:8080/v1/update?image=foo/bar:1.0,foo/baz:latest
```

##### Image Filtering without Tags

If no tag is provided, Watchtower matches containers regardless of their tag.

The following `curl` command would trigger an update for the images `foo/bar` and `foo/baz`:

```bash
curl -H "Authorization: Bearer mytoken" localhost:8080/v1/update?image=foo/bar,foo/baz
```

#### Scheduled Updates

By default, enabling this mode prevents periodic polls (i.e. [scheduled](../../configuration/arguments/index.md#schedule) or [interval](../../configuration/arguments/index.md#poll_interval) polling).
Use the [HTTP API Periodic Polls](../../configuration/arguments/index.md#http_api_periodic_polls) configuration option to allow both API-triggered and scheduled updates.

#### Example

```yaml title="Example Docker Compose Configuration"
services:
  app-monitored-by-watchtower:
    image: myapps/monitored-by-watchtower
    labels:
      - "com.centurylinklabs.watchtower.enable=true"

  watchtower:
    image: nickfedor/watchtower
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    command: --http-api-update --http-api-metrics
    environment:
      - WATCHTOWER_HTTP_API_TOKEN=mytoken
    labels:
      - "com.centurylinklabs.watchtower.enable=false"
    ports:
      - 8080:8080
    restart: unless-stopped
```

!!! Note
    Both `--http-api-update` and `--http-api-metrics` can be enabled simultaneously to provide both update triggering and monitoring capabilities.
