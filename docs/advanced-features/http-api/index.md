# HTTP API

## Overview

Watchtower has an [optional](../../configuration/arguments/index.md#http_api_mode) HTTP API server.

!!! Caution
    This is a relatively simple API with significant security implications.

## Endpoints

|              **Name**              | **Method** | **Endpoint**  |                           **Parameters**                            |                           **Description**                            |
|:----------------------------------:|:----------:|:-------------:|:-------------------------------------------------------------------:|:--------------------------------------------------------------------:|
|     [Update](#http_api_update)     |   `POST`   | `/v1/update`  | [`image`](#image_parameter_usage), [`async`](#asynchronous_updates) | Triggers container updates and returns JSON results of the operation |
| [Metrics](../metrics-api/index.md) |   `GET`    | `/v1/metrics` |                                                                     |  Exposes Prometheus-compatible metrics for monitoring and alerting   |

!!! Note
    Endpoints enforce HTTP method restrictions using method-based routing.
    Requests with unsupported methods will receive a `405 Method Not Allowed` response.

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

**Full Updates (no `?image=` parameter):**

- Returns HTTP 429 immediately if another update is already in progress
- Includes a `Retry-After: 30` header suggesting when to retry the request
- Does not block or wait for the existing update to complete

**Targeted Updates (with `?image=` parameter):**

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
- All HTTP API endpoints (`/v1/update` and `/v1/metrics`) require an `Authorization: Bearer <token>` header with the predefined HTTP API token value.
- Invalid token attempts for any endpoint requiring auth (`/v1/update` and `/v1/metrics`) are logged with the token length (not the token value)

##### Rate Limiting

Watchtower enforces two independent mechanisms that can each return HTTP 429 (Too Many Requests):

**Per-IP request-rate limiting** (applies globally to all HTTP API endpoints):

- Every incoming request to any HTTP API endpoint (`/v1/update`, `/v1/metrics`, etc.) is checked against a per-IP rate limiter **before** authentication is evaluated.
- Default limit: 60 requests per minute with a burst capacity of 10 requests.
- Configurable via [`--http-api-rate-limit`](../../configuration/arguments/index.md#http_api_rate_limit) flag or `WATCHTOWER_HTTP_API_RATE_LIMIT` environment variable.
- Rate-limited requests receive HTTP 429 with no body.
- Rate limit state is tracked per client IP address.

**Concurrency-based update limiting** (applies only to `/v1/update`):

- The `/v1/update` handler uses an internal lock to ensure only one update runs at a time.
- If a full update (no `image` query parameter) is requested while another update is already in progress, the handler immediately returns HTTP 429 with a JSON error body and a `Retry-After: 30` header.
- Targeted updates (with `image` query parameter) block until the lock is available rather than returning 429.

**Precedence:** Per-IP rate limiting is evaluated first. If a request passes the rate limit, it proceeds to the endpoint handler where concurrency limiting may apply for `/v1/update`.

##### Request Body Protection

- Request bodies are capped at 1 MiB to prevent resource exhaustion from large uploads
- Requests exceeding this limit will be rejected with HTTP 413 (Payload Too Large)

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
