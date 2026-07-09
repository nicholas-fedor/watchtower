# HTTP API

## Deprecation Notice

!!! Warning "Watchtower v2 Legacy HTTP API Configuration Deprecation"
    Endpoint-specific configuration options will be removed with the release of Watchtower v2.

    Use the [`HTTP API Endpoints`](#http_api_endpoints) configuration option instead.

## HTTP API Host

Sets the host interface for binding the HTTP API.

```text
            Argument: --http-api-host
Environment Variable: WATCHTOWER_HTTP_API_HOST
                Type: String
             Default: empty (binds to all interfaces)
```

!!! Note "See the [HTTP API Host documentation](../../http-api/configuration/host-and-port/index.md#http_api_host) for details"

## HTTP API Port

Sets the listening port for the HTTP API.

```text
            Argument: --http-api-port
Environment Variable: WATCHTOWER_HTTP_API_PORT
                Type: String
             Default: 8080
```

!!! Note "See the [HTTP API Port documentation](../../http-api/configuration/host-and-port/index.md#http_api_port) for details"

## HTTP API Token

Sets an authentication token for HTTP API requests.

```text
            Argument: --http-api-token
Environment Variable: WATCHTOWER_HTTP_API_TOKEN
                Type: String
             Default: None
```

!!! Note
    Supports file path for Docker Secrets (e.g., `/run/secrets/http_api_token`).

## HTTP API Events Token

Sets an authentication token with read-only permissions specifically for the events SSE endpoint (`/v1/events`).

```text
            Argument: --http-api-events-token
Environment Variable: WATCHTOWER_HTTP_API_EVENTS_TOKEN
                Type: String
             Default: None
```

This is a separate token from the [`http-api-token`](#http_api_token) in order to reduce credential exposure, because events tokens may be exposed in URL query parameters.

The token can be provided via the `Authorization` header (for programmatic clients) or as the `access_token` query parameter (for browser `EventSource` which cannot set custom headers).

!!! Important "This is **required** when the `events` endpoint is enabled via [`http-api-endpoints`](#http_api_endpoints)."

!!! Note
    Supports file path for Docker Secrets (e.g., `/run/secrets/http_api_events_token`).

## HTTP API Endpoints

Selects which HTTP API endpoints to enable.

```text
            Argument: --http-api-endpoints
Environment Variable: WATCHTOWER_HTTP_API_ENDPOINTS
                Type: String (comma or space separated list)
             Default: empty (HTTP API disabled)
```

Valid names (case-insensitive):

| Name | Routes | Auth |
|------|--------|------|
| [`health`](../../http-api/endpoints/health/index.md) | `/livez`, `/readyz`, `/startupz` | None |
| [`update`](../../http-api/endpoints/update/index.md) | `POST /v1/update` | [`http-api-token`](#http_api_token) |
| [`metrics`](../../http-api/endpoints/metrics/index.md) | `GET /v1/metrics`, [`GET /v1/status`](../../http-api/endpoints/status/index.md) | [`http-api-token`](#http_api_token) |
| [`containers`](../../http-api/endpoints/containers/index.md) | `GET /v1/containers`, [`/v1/containers/details`](../../http-api/endpoints/container-details/index.md) | [`http-api-token`](#http_api_token) |
| [`check`](../../http-api/endpoints/check/index.md) | `POST /v1/check` | [`http-api-token`](#http_api_token) |
| [`history`](../../http-api/endpoints/history/index.md) | `GET /v1/history` | [`http-api-token`](#http_api_token) |
| [`images`](../../http-api/endpoints/images/index.md) | `GET /v1/images` | [`http-api-token`](#http_api_token) |
| [`config`](../../http-api/endpoints/config/index.md) | `GET /v1/config` | [`http-api-token`](#http_api_token) |
| [`events`](../../http-api/endpoints/events/index.md) | `GET /v1/events` | [`http-api-events-token`](#http_api_events_token) |
| [`swagger`](../../http-api/endpoints/swagger/index.md) | `GET /swagger/*` | None |

!!! Warning
    The `all` value enables every endpoint and **MUST** be the only value.

!!! Note "Defining multiple endpoints"
    - The CLI flag can be specified multiple times.
    - Defining the environment variable multiple times will not work (only the last value is used).
    - For environment variables, use a single comma- or space-separated value, or a YAML array.

Examples:

```bash
# Metrics and health probes
WATCHTOWER_HTTP_API_ENDPOINTS=health,metrics
WATCHTOWER_HTTP_API_TOKEN=...

# Full surface
WATCHTOWER_HTTP_API_ENDPOINTS=all
WATCHTOWER_HTTP_API_TOKEN=...
WATCHTOWER_HTTP_API_EVENTS_TOKEN=...

# Update API only (API-only mode unless periodic polls are also enabled)
WATCHTOWER_HTTP_API_ENDPOINTS=update
WATCHTOWER_HTTP_API_TOKEN=...
```

!!! Note
    - Health is **not** added automatically; include `health` when you need probes.
    - Protected `/v1/*` endpoints require [`http-api-token`](#http_api_token).
    - Enabling `events` requires [`http-api-events-token`](#http_api_events_token).
    - See per-endpoint docs under [HTTP API](../../http-api/overview/index.md).

!!! Warning "Invalid values stop startup"
    Watchtower exits on startup if:

    - An endpoint name is not in the table above
    - `all` is combined with any other name

    The error message includes the invalid value and the list of valid names.

## HTTP API Rate Limit

Sets the maximum number of API requests allowed per minute per IP address.

```text
            Argument: --http-api-rate-limit
Environment Variable: WATCHTOWER_HTTP_API_RATE_LIMIT
                Type: Integer
             Default: 60
```

!!! Note
    When the limit is exceeded, the client receives HTTP 429 (Too Many Requests).

## HTTP API TLS Certificate

Path to the TLS certificate file for the HTTP API.

```text
            Argument: --http-api-tls-cert
Environment Variable: WATCHTOWER_HTTP_API_TLS_CERT
                Type: String
             Default: empty (HTTP)
```

!!! Important
    When both the [`http-api-tls-cert`](#http_api_tls_certificate) and [`http-api-tls-key`](#http_api_tls_key) are provided, the server uses HTTPS.

## HTTP API TLS Key

Path to the TLS private key file for the HTTP API.

```text
            Argument: --http-api-tls-key
Environment Variable: WATCHTOWER_HTTP_API_TLS_KEY
                Type: String
             Default: empty (HTTP)
```

!!! Important
    When both the [`http-api-tls-cert`](#http_api_tls_certificate) and [`http-api-tls-key`](#http_api_tls_key) are provided, the server uses HTTPS.

!!! Note "See the [HTTP API Host documentation](../../http-api/configuration/tls/index.md) for details"

## HTTP API Trusted Proxies

Comma-separated list of trusted proxy IP addresses or CIDR ranges for reverse proxy support.
When set, enables proxy header processing for client IP and scheme detection.

```text
            Argument: --http-api-trusted-proxies
Environment Variable: WATCHTOWER_HTTP_API_TRUSTED_PROXIES
                Type: String Array
             Default: (unset)
```

!!! Important
    Required for correct client IP detection and rate limiting behind a reverse proxy (Traefik, Caddy, Nginx, etc.).

## HTTP API Proxy Header

Header to read the real client IP from when behind a reverse proxy.

```text
            Argument: --http-api-proxy-header
Environment Variable: WATCHTOWER_HTTP_API_PROXY_HEADER
                Type: String
             Default: X-Forwarded-For
```

!!! Note
    Only used when `--http-api-trusted-proxies` is set.

Common values:

- `X-Forwarded-For` (Traefik, Caddy)
- `X-Real-IP` (Nginx)
- `CF-Connecting-IP` (Cloudflare)

## HTTP API CORS Origins

Comma-separated list of allowed CORS origins for cross-origin requests.
Use this to restrict which origins can access the API.

```text
            Argument: --http-api-cors-origins
Environment Variable: WATCHTOWER_HTTP_API_CORS_ORIGINS
                Type: String Array
             Default: Unset (same-origin only)
```

!!! Note
    When unset, CORS is disabled and only same-origin requests are allowed.
    Set this to permit specific cross-origin origins.

## Deprecated Configuration Options

/// details | The following legacy configuration options are deprecated and will be removed with the release of Watchtower v2.
    type: warning

!!! Warning
    - Legacy flags still work alone or together with [`http-api-endpoints`](#http_api_endpoints); values are **unioned** and **deduplicated**.
    - Multiple legacy options combine (for example `http-api-update` + `http-api-metrics` → `update,metrics`).
    - Example mix: `WATCHTOWER_HTTP_API_ENDPOINTS=health,check` with `WATCHTOWER_HTTP_API_UPDATE=true` → `health,check,update`.
    - Prefer migrating fully to the allowlist; legacy flags will be removed in Watchtower v2.

### HTTP API Update

Runs Watchtower in HTTP API mode, so that image updates must be triggered by a request.

```text
            Argument: --http-api-update
Environment Variable: WATCHTOWER_HTTP_API_UPDATE
                Type: Boolean
             Default: false
```

!!! Deprecated
    Add `update` to the  [`http-api-endpoints`](#http_api_endpoints) configuration instead.

### HTTP API Metrics

Runs Watchtower with the Prometheus metrics API enabled.

```text
            Argument: --http-api-metrics
Environment Variable: WATCHTOWER_HTTP_API_METRICS
                Type: Boolean
             Default: false
```

!!! Deprecated
    Add `metrics` to the  [`http-api-endpoints`](#http_api_endpoints) configuration instead.

### HTTP API Containers

Runs Watchtower with the read-only containers API enabled.

```text
            Argument: --http-api-containers
Environment Variable: WATCHTOWER_HTTP_API_CONTAINERS
                Type: Boolean
             Default: false
```

!!! Deprecated
    Add `containers` to the  [`http-api-endpoints`](#http_api_endpoints) configuration instead.

///
