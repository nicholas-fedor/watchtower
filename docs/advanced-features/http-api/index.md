# HTTP API

## Overview

Watchtower has an [optional](../../configuration/arguments/index.md#http_api_mode) HTTP API server.

!!! Caution
    This is a relatively simple API with significant security implications.

## Endpoints

|                     **Name**                     | **Method** |       **Endpoint**       | **Auth** |                                                 **Parameters**                                                 |                                              **Description**                                              |
|:------------------------------------------------:|:----------:|:------------------------:|:--------:|:--------------------------------------------------------------------------------------------------------------:|:---------------------------------------------------------------------------------------------------------:|
|            [Update](#http_api_update)            |   `POST`   |       `/v1/update`       |   Yes    | [`image`](#image-parameter-usage), [`container`](#container-parameter-usage), [`async`](#asynchronous-updates) |                   Triggers container updates and returns JSON results of the operation                    |
|             [Check](#http_api_check)             |   `POST`   |       `/v1/check`        |   Yes    |                  [`image`](#image-parameter-usage), [`container`](#container-parameter-usage)                  |                   Checks containers for available updates without pulling or restarting                   |
|        [Containers](#http_api_containers)        |   `GET`    |     `/v1/containers`     |   Yes    |                       [`name`](#name-parameter-usage), [`image`](#image-parameter-usage)                       |                     Lists watched containers and their current running image digests                      |
| [Container Details](#http_api_container_details) |   `GET`    | `/v1/containers/details` |   Yes    |                       [`name`](#name-parameter-usage), [`image`](#image-parameter-usage)                       | Returns detailed information about each watched container including running state and configuration flags |
|           [History](#http_api_history)           |   `GET`    |      `/v1/history`       |   Yes    |    [`since`](#since-parameter-usage), [`until`](#until-parameter-usage), [`limit`](#limit-parameter-usage)     |            Returns historical scan results from the in-memory ring buffer (up to 500 entries)             |
|            [Images](#http_api_images)            |   `GET`    |       `/v1/images`       |   Yes    |                          [`name`](#name-parameter-usage), [`id`](#id-parameter-usage)                          |                   Lists tracked images with their current digests and container counts                    |
|            [Config](#http_api_config)            |   `GET`    |       `/v1/config`       |   Yes    |                                                                                                                |                           Returns the active Watchtower configuration settings                            |
|            [Events](#http_api_events)            |   `GET`    |       `/v1/events`       |   Yes    |                                                                                                                |                        Streams real-time operational events via Server-Sent Events                        |
|            [Status](#http_api_status)            |   `GET`    |       `/v1/status`       |   Yes    |                                                                                                                |                                Returns the summary of the most recent scan                                |
|        [Metrics](../metrics-api/index.md)        |   `GET`    |      `/v1/metrics`       |   Yes    |                                                                                                                |                     Exposes Prometheus-compatible metrics for monitoring and alerting                     |
|           [Swagger](#http-api-swagger)           |   `GET`    |       `/swagger/*`       |    No    |                                                                                                                |                               Interactive API documentation via Swagger UI                                |
|                     Liveness                     |   `GET`    |         `/livez`         |    No    |                                                                                                                |                                Returns `200 OK` when the server is running                                |
|                    Readiness                     |   `GET`    |        `/readyz`         |    No    |                                                                                                                |                     Returns `200 OK` when Docker client is connected, `503` otherwise                     |
|                     Startup                      |   `GET`    |       `/startupz`        |    No    |                                                                                                                |                               Returns `200 OK` once the server has started                                |

!!! Note
    Endpoints enforce HTTP method restrictions using method-based routing.
    Requests with unsupported methods will receive a `405 Method Not Allowed` response.

### HTTP API Swagger

To enable the Swagger UI, use the `--http-api-swagger` CLI argument or the `WATCHTOWER_HTTP_API_SWAGGER` environment variable.

### HTTP API Update

To enable this mode, use the `--http-api-update` CLI argument or the `WATCHTOWER_HTTP_API_UPDATE` environment variable.

#### Response Format

The `/v1/update` endpoint returns a JSON response containing the results of the update operation:

```json
{
  "summary": {
    "scanned": 8,
    "updated": 0,
    "failed": 0,
    "restarted": 0,
    "skipped": 2
  },
  "timing": {
    "duration_ms": 1250,
    "duration": "1.25s"
  },
  "timestamp": "2025-01-20T11:30:45Z",
  "api_version": "v1"
}
```

##### Summary Section

- `scanned`: Number of containers that were scanned for updates
- `updated`: Number of containers that were successfully updated
- `failed`: Number of containers where the update failed
- `restarted`: Number of containers that were restarted due to linked dependencies
- `skipped`: Number of containers that were skipped during the update

##### Timing Section

- `duration_ms`: Execution time in milliseconds
- `duration`: Human-readable execution time

##### Metadata

- `timestamp`: UTC timestamp when the response was generated (RFC3339 format)
- `api_version`: API version identifier

#### HTTP Status Codes

The `/v1/update` endpoint returns the following HTTP status codes:

| Status Code | Description                                                                               |
|:-----------:|:------------------------------------------------------------------------------------------|
|     200     | Update completed successfully                                                             |
|     202     | Update triggered successfully and running asynchronously (with `?async=true`)             |
|     401     | Invalid or missing authentication token                                                   |
|     408     | Update handler timed out (exceeded 10-minute limit)                                       |
|     429     | Another update is already in progress (full updates only) or the request was rate limited |
|     500     | Internal server error during request processing                                           |
|     503     | Client cancelled while waiting on update lock (targeted updates only)                     |

#### Error Response Format

When an error occurs, the API returns a JSON response with the following structure:

```json
{
  "error": "another update is already running",
  "api_version": "v1",
  "timestamp": "2025-01-20T11:30:45Z"
}
```

- `error`: A human-readable error message describing what went wrong
- `api_version`: API version identifier
- `timestamp`: UTC timestamp when the error response was generated (RFC3339 format)

#### Concurrency Behavior

The `/v1/update` endpoint handles concurrent requests differently based on whether targeted or full updates are being performed:

**Full Updates (no `?image=` or `?container=` parameter):**

- Returns HTTP 429 immediately if another update is already in progress
- Includes a `Retry-After: 30` header suggesting when to retry the request
- Does not block or wait for the existing update to complete

**Targeted Updates (with `?image=` or `?container=` parameter):**

- Blocks until the update lock is available
- Waits for any in-progress update to complete before proceeding
- Does not return HTTP 429

This behavior ensures that full updates (which may be resource-intensive) are not queued up, while targeted updates (which are typically faster) can wait for their turn.

#### Asynchronous Updates

The `/v1/update` endpoint supports an `async` query parameter to trigger updates without waiting for completion. This is useful for CI environments or automation that needs to fire-and-forget without maintaining a long-lived connection.

##### Asynchronous Update Trigger

Adding the `?async=true` parameter to a POST request causes the handler to spawn the update in a background goroutine and return immediately with HTTP 202 Accepted.

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/update?async=true"
```

Response:

```http
HTTP/1.1 202 Accepted
Content-Type: application/json
```

Equivalent example for a targeted async update:

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/update?image=foo/bar:latest&async=true"
```

The same concurrency behavior applies to async requests: full updates return 429 if another update is already in progress, while targeted updates block until the lock is available before spawning the async goroutine.

##### Status Codes for Async Requests

| Status Code | Description                                                                               |
|:-----------:|:------------------------------------------------------------------------------------------|
|     202     | Update triggered successfully and running asynchronously                                  |
|     401     | Invalid or missing authentication token                                                   |
|     408     | Update handler timed out (exceeded 10-minute limit)                                       |
|     429     | Another update is already in progress (full updates only) or the request was rate limited |
|     500     | Internal server error during request processing                                           |
|     503     | Client cancelled while waiting on update lock (targeted updates only)                     |

The following example shows what happens when a full update is requested while another update is already running:

```bash
curl -i -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/update"
```

Response:

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 30

{
  "error": "another update is already running",
  "api_version": "v1",
  "timestamp": "2025-01-20T11:30:45Z"
}
```

The client should wait at least 30 seconds (as indicated by the `Retry-After` header) before attempting another request.

#### Security

##### Authentication

Watchtower uses token-based, header authentication for the HTTP API.

- This should be set using the [HTTP API Token](../../configuration/arguments/index.md#http_api_token) configuration option.
- All authenticated HTTP API endpoints (`/v1/update`, `/v1/check`, `/v1/containers`, `/v1/containers/details`, `/v1/history`, `/v1/images`, `/v1/config`, `/v1/status`, `/v1/metrics`, `/v1/events`) require an `Authorization: Bearer <token>` header with the predefined HTTP API token value.
- Invalid token attempts are logged with the client IP address.
- Health probe endpoints (`/livez`, `/readyz`, `/startupz`) do not require authentication.

##### Rate Limiting

Watchtower enforces two independent mechanisms that can each return HTTP 429 (Too Many Requests):

**Per-IP request-rate limiting** (applies globally to all HTTP API endpoints):

- Every incoming request is checked against a per-IP rate limiter using a **sliding window** algorithm **before** authentication is evaluated.
- Default limit: 60 requests per minute.
- Configurable via [`--http-api-rate-limit`](../../configuration/arguments/index.md#http_api_rate_limit) flag or `WATCHTOWER_HTTP_API_RATE_LIMIT` environment variable.
- Rate-limited requests receive HTTP 429 with no body.
- Rate limit state is tracked per client IP address.

**Concurrency-based update limiting** (applies only to `/v1/update`):

- The `/v1/update` handler uses an internal lock to ensure only one update runs at a time.
- If a full update (no `image` or `container` query parameter) is requested while another update is already in progress, the handler immediately returns HTTP 429 with a JSON error body and a `Retry-After: 30` header.
- Targeted updates (with `image` or `container` query parameter) block until the lock is available rather than returning 429.

**Precedence:** Per-IP rate limiting is evaluated first. If a request passes the rate limit, it proceeds to the endpoint handler where concurrency limiting may apply for `/v1/update`.

##### Request Body Protection

- Request bodies are capped at 1 MiB to prevent resource exhaustion from large uploads
- Requests exceeding this limit will be rejected with HTTP 413 (Payload Too Large)

##### Update Handler Timeout

- The `/v1/update` handler has a 10-minute timeout enforced by Fiber's timeout middleware.
- The timeout covers the full lifecycle: waiting for the concurrency lock, performing the container update scan, and returning results.
- When the timeout is exceeded, the handler returns HTTP 408 (Request Timeout) and the handler goroutine is abandoned.
- The handler listens on `c.Context().Done()` for cooperative cancellation — targeted updates waiting for the lock will return HTTP 503 when the request context is cancelled.

#### Address and Port Configuration

Watchtower defaults to listening on all interfaces on port 8080.

##### HTTP API Host

Use the [HTTP API Host](../../configuration/arguments/index.md#http_api_host) configuration option to bind to a specific host interface.

- This must be a valid IP address (IPv4 or IPv6).
- If not specified, Watchtower listens on all interfaces on the port specified by `--http-api-port`.

##### HTTP API Port

The port can be changed using the [HTTP API Port](../../configuration/arguments/index.md#http_api_port) configuration option.

If Watchtower is being run via a Docker container, then the `host:container` port mapping can be updated accordingly (e.g. `8080:8080` -> `9000:8080`).

##### Examples

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
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/update"
```

##### Image Filtering with Tags

You can specify image tags to target containers running a specific version (e.g., `foo/bar:1.0`).

For example, to update only containers using `foo/bar:1.0` and `foo/baz:latest`:

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/update?image=foo/bar:1.0,foo/baz:latest"
```

##### Image Filtering without Tags

If no tag is provided, Watchtower matches containers regardless of their tag.

The following `curl` command would trigger an update for the images `foo/bar` and `foo/baz`:

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/update?image=foo/bar,foo/baz"
```

#### Container Parameter Usage

Watchtower supports using the `container` URL query parameter to filter updates for only certain containers by name.

##### Container Name Patterns

You can specify exact container names or Go regex patterns to match containers.

For example, to update only containers with names starting with `web-`:

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/update?container=^web-.*"
```

##### Multiple Container Patterns

Use commas to specify multiple patterns:

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/update?container=^web-.*,^api-.*"
```

##### Regex Pattern Matching

Container patterns support Go regex syntax. Invalid regex patterns are treated as literal strings for exact matching.

| Pattern       | Matches                          |
|:--------------|:---------------------------------|
| `mycontainer` | Exact match for `mycontainer`    |
| `^web-.*`     | Any name starting with `web-`    |
| `.*-prod$`    | Any name ending with `-prod`     |

#### Using the HTTP API and Periodic Updates

By default, enabling the HTTP API prevents periodic updates (i.e. [scheduled](../../configuration/arguments/index.md#schedule) or [interval](../../configuration/arguments/index.md#poll_interval) polling).

Use the [HTTP API Periodic Polls](../../configuration/arguments/index.md#http_api_periodic_polls) configuration option to enable periodic updates while using the HTTP API.

##### Example

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

!!! Warning
    Enabling the HTTP API with port mappings will automatically disable Watchtower's self-update functionality to prevent port conflicts during container recreation. See [Updating Watchtower](../../getting-started/updating-watchtower/index.md#port-configuration-limitation) for more details.

### HTTP API Check

To enable this read-only endpoint, use the `--http-api-check` CLI argument or the `WATCHTOWER_HTTP_API_CHECK` environment variable.

It checks each watched container for available image updates without pulling or restarting, so an external orchestrator can determine what would change before triggering an update.

#### Response Format

The `/v1/check` endpoint returns a JSON array of container check results:

```json
{
    "containers": [
        {
            "name": "nginx",
            "image": "nginx:latest",
            "has_update": true,
            "latest_image": "nginx:1.27"
        }
    ],
    "count": 1,
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `name`: Container name
- `image`: Current image reference with tag
- `has_update`: Whether a newer image is available
- `latest_image`: The newest available image reference (empty if no update available)

#### HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | Check completed successfully                   |
|     401     | Invalid or missing authentication token        |
|     500     | Internal server error during request processing|

#### Parameters

##### Image Parameter Usage

The `image` parameter filters the check to only include containers running specific images.

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/check?image=foo/bar:1.0"
```

##### Container Parameter Usage

The `container` parameter filters the check to only include specific containers by name.

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/check?container=nginx"
```

### HTTP API Containers

To enable this read-only endpoint, use the `--http-api-containers` CLI argument or the `WATCHTOWER_HTTP_API_CONTAINERS` environment variable.

It lists the containers Watchtower watches along with their current image identity, so an external orchestrator can compare what is actually running against a registry without pulling any image layers.

#### Response Format

The `/v1/containers` endpoint returns a JSON array of watched containers:

```json
{
    "containers": [
        {
            "name": "nginx",
            "image": "nginx:latest",
            "image_id": "sha256:1111...",
            "digest": "sha256:2222..."
        }
    ],
    "count": 1,
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `name`: Container name
- `image`: Image reference with tag
- `image_id`: Local image config ID
- `digest`: Registry manifest digest the image was pulled from (from the image's `RepoDigests`), directly comparable to a registry's `Docker-Content-Digest`. Empty for locally-built images with no registry reference.

!!! Note
    `--http-api-containers` can be enabled alongside `--http-api-update` and `--http-api-metrics`.

#### Name Parameter Usage

The `name` parameter filters results to a specific container by exact name.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/containers?name=nginx"
```

#### Image Parameter Usage

The `image` parameter filters results to containers running a specific image.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/containers?image=nginx:latest"
```

### HTTP API Status

To enable this read-only endpoint, use the `--http-api-metrics` CLI argument or the `WATCHTOWER_HTTP_API_METRICS` environment variable (the status endpoint is enabled alongside metrics).

It returns a summary of the most recent Watchtower scan, including counts of scanned, updated, failed, restarted, and skipped containers.

#### Response Format

The `/v1/status` endpoint returns a JSON scan summary:

```json
{
    "summary": {
        "scanned": 8,
        "updated": 0,
        "failed": 0,
        "restarted": 0,
        "skipped": 2
    },
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `scanned`: Number of containers scanned
- `updated`: Number of containers successfully updated
- `failed`: Number of containers where the update failed
- `restarted`: Number of containers restarted
- `skipped`: Number of containers skipped
- `timestamp`: UTC timestamp of the last scan (RFC3339 format)

If no scan has been performed yet, the endpoint returns HTTP 204 (No Content).

#### HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | Status retrieved successfully                  |
|     204     | No scan has been performed yet                 |
|     401     | Invalid or missing authentication token        |
|     500     | Internal server error during request processing|

### HTTP API Container Details

To enable this read-only endpoint, use the `--http-api-containers` CLI argument or the `WATCHTOWER_HTTP_API_CONTAINERS` environment variable (container details are enabled alongside the containers endpoint).

It returns detailed information about each watched container, including running state, image identity, and configuration flags.

#### Response Format

The `/v1/containers/details` endpoint returns a JSON array of container details:

```json
{
    "containers": [
        {
            "name": "nginx",
            "image": "nginx:latest",
            "image_id": "sha256:1111...",
            "digest": "sha256:2222...",
            "running": true,
            "watchtower": false,
            "monitor_only": false,
            "no_pull": false,
            "enabled": true,
            "stale": false,
            "scope": ""
        }
    ],
    "count": 1,
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `name`: Container name
- `image`: Image reference with tag
- `image_id`: Local image config ID (sha256:...)
- `digest`: Registry manifest digest (sha256:...). Empty for locally-built images.
- `running`: Whether the container is currently running
- `watchtower`: Whether this is the Watchtower container itself
- `monitor_only`: Whether the container is in monitor-only mode
- `no_pull`: Whether image pulling is disabled for this container
- `enabled`: Whether the container is enabled for watching
- `stale`: Whether the container's image is outdated
- `scope`: Monitoring scope of the container

#### Name Parameter Usage

The `name` parameter filters results to a specific container by exact name.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/containers/details?name=nginx"
```

#### Image Parameter Usage

The `image` parameter filters results to containers running a specific image.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/containers/details?image=nginx:latest"
```

#### HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | Container details retrieved successfully       |
|     401     | Invalid or missing authentication token        |
|     500     | Internal server error during request processing|

### HTTP API History

To enable this read-only endpoint, use the `--http-api-history` CLI argument or the `WATCHTOWER_HTTP_API_HISTORY` environment variable.

It returns historical scan results from an in-memory ring buffer (up to 500 entries).

#### Response Format

The `/v1/history` endpoint returns a JSON object with scan history entries:

```json
{
    "entries": [
        {
            "timestamp": "2025-01-20T11:30:45Z",
            "scanned": 8,
            "updated": 0,
            "failed": 0,
            "restarted": 0,
            "skipped": 2
        }
    ],
    "count": 1,
    "timestamp": "2025-01-20T11:35:00Z",
    "api_version": "v1"
}
```

#### Parameters

##### Since Parameter Usage

The `since` parameter filters entries to those at or after the specified RFC3339 timestamp.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/history?since=2025-01-20T11:00:00Z"
```

##### Until Parameter Usage

The `until` parameter filters entries to those at or before the specified RFC3339 timestamp.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/history?until=2025-01-20T12:00:00Z"
```

##### Limit Parameter Usage

The `limit` parameter restricts the maximum number of entries returned.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/history?limit=10"
```

##### Combined Parameters

Parameters can be combined to query a specific time range with a limit:

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/history?since=2025-01-20T11:00:00Z&until=2025-01-20T12:00:00Z&limit=50"
```

#### HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | History retrieved successfully                 |
|     400     | Invalid query parameter                        |
|     401     | Invalid or missing authentication token        |
|     500     | Internal server error during request processing|

### HTTP API Images

To enable this read-only endpoint, use the `--http-api-images` CLI argument or the `WATCHTOWER_HTTP_API_IMAGES` environment variable.

It lists the images tracked by Watchtower along with their current digests and container counts.

#### Response Format

The `/v1/images` endpoint returns a JSON object with image statuses:

```json
{
    "images": [
        {
            "name": "nginx:latest",
            "image_id": "sha256:1111...",
            "digest": "sha256:2222...",
            "containers": 3
        }
    ],
    "count": 1,
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `name`: Image name with tag
- `image_id`: Local image config ID (sha256:...)
- `digest`: Registry manifest digest (sha256:...). Empty for locally-built images.
- `containers`: Number of watched containers using this image

#### Name Parameter Usage

The `name` parameter filters results to a specific image by exact name.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/images?name=nginx:latest"
```

#### ID Parameter Usage

The `id` parameter filters results to a specific image by its image ID.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/images?id=sha256:1111..."
```

#### HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | Images retrieved successfully                  |
|     401     | Invalid or missing authentication token        |
|     500     | Internal server error during request processing|

### HTTP API Config

To enable this read-only endpoint, use the `--http-api-config` CLI argument or the `WATCHTOWER_HTTP_API_CONFIG` environment variable.

It returns the active Watchtower configuration settings. Sensitive values (notification URLs, tokens) are redacted.

#### Response Format

The `/v1/config` endpoint returns the current configuration:

```json
{
    "config": {
        "monitor_only": false,
        "cleanup": true,
        "no_pull": false,
        "no_restart": false,
        "rolling_restart": false,
        "include_stopped": false,
        "include_restarting": false,
        "lifecycle_hooks": false,
        "label_enable": false,
        "filter_desc": "",
        "scope": ""
    },
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `monitor_only`: Whether Watchtower is in monitor-only mode
- `cleanup`: Whether old images are removed after updating
- `no_pull`: Whether image pulling is disabled
- `no_restart`: Whether container restarting is disabled
- `rolling_restart`: Whether containers are restarted one at a time
- `include_stopped`: Whether stopped containers are included
- `include_restarting`: Whether restarting containers are included
- `lifecycle_hooks`: Whether lifecycle hooks are enabled
- `label_enable`: Whether label-based enabling is active
- `filter_desc`: Human-readable description of the applied filter
- `scope`: Monitoring scope

#### HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | Configuration retrieved successfully           |
|     401     | Invalid or missing authentication token        |
|     500     | Internal server error during request processing|

### HTTP API Events

To enable this endpoint, use the `--http-api-events` CLI argument or the `WATCHTOWER_HTTP_API_EVENTS` environment variable.

It streams Watchtower operational events (scan started/completed, update started/completed/failed) via Server-Sent Events (SSE).

#### Authentication

The events endpoint requires authentication via the `--http-api-events-token` flag (or the `WATCHTOWER_HTTP_API_EVENTS_TOKEN` environment variable).

This token can be provided in two formats:

- **Header-based auth** — For programmatic clients using `curl`, `fetch`, etc.:

  ```bash
  curl -N -H "Authorization: Bearer my-events-token" "http://localhost:8080/v1/events"
  ```

- **Query-parameter auth** — For browser `EventSource` API, which cannot send custom headers:

  ```javascript
  const eventSource = new EventSource('http://localhost:8080/v1/events?access_token=my-events-token');

  eventSource.addEventListener('scan_started', (e) => {
      console.log('Scan started:', JSON.parse(e.data));
  });

  eventSource.addEventListener('scan_completed', (e) => {
      console.log('Scan completed:', JSON.parse(e.data));
  });
  ```

  ```bash
  curl -N "http://localhost:8080/v1/events?access_token=my-events-token"
  ```

The events endpoint uses a separate token from the main API token to limit blast radius, since query parameters may appear in access logs, browser history, and proxy logs.

#### Event Format

Each event is a Server-Sent Event with an event type and JSON data payload:

```
event: scan_completed
data: {"type":"scan_completed","timestamp":"2025-01-20T11:30:45Z","data":{"scanned":8,"updated":0}}
```

#### HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | Event stream established                       |
|     401     | Invalid or missing authentication token        |
|     403     | Origin not allowed                              |
