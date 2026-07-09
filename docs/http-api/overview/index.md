# API

## Overview

Watchtower has an optional HTTP API server. Enable it with the [HTTP API configuration options](../../configuration/http-api/index.md), primarily [`http-api-endpoints`](../../configuration/http-api/index.md#http_api_endpoints).

When no endpoints are selected (and no deprecated legacy enable flags are set), the HTTP API does not start.

## Enabling endpoints

Select which routes to expose with a comma- or space-separated allowlist:

```bash
# Metrics and health probes
WATCHTOWER_HTTP_API_ENDPOINTS=health,metrics
WATCHTOWER_HTTP_API_TOKEN=...

# Full surface
WATCHTOWER_HTTP_API_ENDPOINTS=all
WATCHTOWER_HTTP_API_TOKEN=...
WATCHTOWER_HTTP_API_EVENTS_TOKEN=...
```

| Allowlist name | What it enables                                     |
|----------------|-----------------------------------------------------|
| `health`       | Liveness, readiness, and startup probes             |
| `update`       | Trigger updates                                     |
| `metrics`      | Prometheus metrics and scan status                  |
| `containers`   | Container list and details                          |
| `check`        | Dry-run update checks                               |
| `history`      | Scan history                                        |
| `images`       | Tracked images                                      |
| `config`       | Active configuration (secrets redacted)             |
| `events`       | Real-time SSE event stream                          |
| `swagger`      | Swagger UI                                          |
| `all`          | Every endpoint above (`all` must be the only value) |

See [HTTP API Endpoints](../../configuration/http-api/index.md#http_api_endpoints) for full flag/env details, multi-value syntax, validation rules, and deprecated legacy flags.

## Security considerations

Watchtower was originally designed to be a stateless application with direct access to the Docker socket.
By having direct access to the Docker socket, the application effectively runs with root-level privileges.
It is worth taking a moment to review and familiarize yourself with the [Docker Engine security documentation](https://docs.docker.com/engine/security/){target="_blank" rel="noopener noreferrer"}.

- !!! Warning "The HTTP API should never be directly exposed to the Internet!"
- !!! Warning "Using the HTTP API without TLS encryption is insecure and not recommended!"
- !!! Warning "Only enable the endpoints that you need!"

Authentication, TLS, trusted proxies, CORS, and rate limiting are covered under [Authentication](../configuration/authentication/index.md), [TLS](../configuration/tls/index.md), and the [HTTP API configuration reference](../../configuration/http-api/index.md).

## Endpoints

|                           **Name**                           | **Enable as** | **Method** |       **Endpoint**       |                                  **Auth**                                   |                                                                                **Parameters**                                                                                |                                              **Description**                                              |
|:------------------------------------------------------------:|:-------------:|:----------:|:------------------------:|:---------------------------------------------------------------------------:|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|:---------------------------------------------------------------------------------------------------------:|
|            [Update](../endpoints/update/index.md)            |   `update`    |   `POST`   |       `/v1/update`       |      [API token](../../configuration/http-api/index.md#http_api_token)      | [`image`](../endpoints/update/index.md#image_name), [`container`](../endpoints/update/index.md#container_name), [`async`](../endpoints/update/index.md#asynchronous_updates) |                   Triggers container updates and returns JSON results of the operation                    |
|             [Check](../endpoints/check/index.md)             |    `check`    |   `POST`   |       `/v1/check`        |      [API token](../../configuration/http-api/index.md#http_api_token)      |                                 [`image`](../endpoints/check/index.md#image_name), [`container`](../endpoints/check/index.md#container_name)                                 |                     Checks containers for available updates via registry digest query                     |
|        [Containers](../endpoints/containers/index.md)        | `containers`  |   `GET`    |     `/v1/containers`     |      [API token](../../configuration/http-api/index.md#http_api_token)      |                              [`name`](../endpoints/containers/index.md#container_name), [`image`](../endpoints/containers/index.md#image_name)                               |                     Lists watched containers and their current running image digests                      |
| [Container Details](../endpoints/container-details/index.md) | `containers`  |   `GET`    | `/v1/containers/details` |      [API token](../../configuration/http-api/index.md#http_api_token)      |                       [`name`](../endpoints/container-details/index.md#container_name), [`image`](../endpoints/container-details/index.md#image_name)                        | Returns detailed information about each watched container including running state and configuration flags |
|           [History](../endpoints/history/index.md)           |   `history`   |   `GET`    |      `/v1/history`       |      [API token](../../configuration/http-api/index.md#http_api_token)      |                [`since`](../endpoints/history/index.md#since), [`until`](../endpoints/history/index.md#until), [`limit`](../endpoints/history/index.md#limit)                |            Returns historical scan results from the in-memory ring buffer (up to 500 entries)             |
|            [Images](../endpoints/images/index.md)            |   `images`    |   `GET`    |       `/v1/images`       |      [API token](../../configuration/http-api/index.md#http_api_token)      |                                       [`name`](../endpoints/images/index.md#image_name), [`id`](../endpoints/images/index.md#image_id)                                       |                   Lists tracked images with their current digests and container counts                    |
|            [Config](../endpoints/config/index.md)            |   `config`    |   `GET`    |       `/v1/config`       |      [API token](../../configuration/http-api/index.md#http_api_token)      |                                                                                                                                                                              |                           Returns the active Watchtower configuration settings                            |
|            [Events](../endpoints/events/index.md)            |   `events`    |   `GET`    |       `/v1/events`       | [Events token](../../configuration/http-api/index.md#http_api_events_token) |                                                                                                                                                                              |                        Streams real-time operational events via Server-Sent Events                        |
|            [Status](../endpoints/status/index.md)            |   `metrics`   |   `GET`    |       `/v1/status`       |      [API token](../../configuration/http-api/index.md#http_api_token)      |                                                                                                                                                                              |                                Returns the summary of the most recent scan                                |
|           [Metrics](../endpoints/metrics/index.md)           |   `metrics`   |   `GET`    |      `/v1/metrics`       |      [API token](../../configuration/http-api/index.md#http_api_token)      |                                                                                                                                                                              |                     Exposes Prometheus-compatible metrics for monitoring and alerting                     |
| [Swagger](../endpoints/swagger/index.md) | `swagger` | `GET` | `/swagger/*` | None | | Interactive API documentation via Swagger UI |
|           [Liveness](../endpoints/health/index.md)           |   `health`    |   `GET`    |         `/livez`         |                                    None                                     |                                                                                                                                                                              |                                Returns `200 OK` when the server is running                                |
|          [Readiness](../endpoints/health/index.md)           |   `health`    |   `GET`    |        `/readyz`         |                                    None                                     |                                                                                                                                                                              |                     Returns `200 OK` when Docker client is connected, `503` otherwise                     |
|           [Startup](../endpoints/health/index.md)            |   `health`    |   `GET`    |       `/startupz`        |                                    None                                     |                                                                                                                                                                              |                               Returns `200 OK` once the server has started                                |

!!! Note
    - Endpoints enforce HTTP method restrictions using method-based routing.
    - Requests with unsupported methods will receive a `405 Method Not Allowed` response.
    - Unknown allowlist names and combining `all` with other names cause Watchtower to exit at startup; see [HTTP API Endpoints](../../configuration/http-api/index.md#http_api_endpoints).
