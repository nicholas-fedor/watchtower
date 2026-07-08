# HTTP API

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

!!! Important "This is **required** when the [`http-api-events`](#http_api_events) endpoint is enabled."

!!! Note
    Supports file path for Docker Secrets (e.g., `/run/secrets/http_api_events_token`).

## HTTP API Full

Enables all of Watchtower's HTTP API endpoints.

```text
            Argument: --http-api-full
Environment Variable: WATCHTOWER_HTTP_API_FULL
                Type: Boolean
             Default: false
```

!!! Note
    -  This requires both the [`HTTP API Token`](#http_api_token) and [`HTTP API Events Token`](#http_api_events_token) to be configured.

## HTTP API Endpoints

### HTTP API Check

Enables a read-only endpoint that checks containers for available updates without pulling or restarting.

```text
            Argument: --http-api-check
Environment Variable: WATCHTOWER_HTTP_API_CHECK
                Type: Boolean
             Default: false
```

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/check/index.md) for details"

### HTTP API Config

Enables the config API endpoint (`/v1/config`).

```text
            Argument: --http-api-config
Environment Variable: WATCHTOWER_HTTP_API_CONFIG
                Type: Boolean
             Default: false
```

!!! Note
    Returns the active Watchtower configuration settings.

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/config/index.md) for details"

### HTTP API Containers

Enables a read-only endpoint that lists watched containers and their current running image digests.

```text
            Argument: --http-api-containers
Environment Variable: WATCHTOWER_HTTP_API_CONTAINERS
                Type: Boolean
             Default: false
```

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/containers/index.md) for details"

### HTTP API Events

Enables the real-time events API endpoint (`/v1/events`).

```text
            Argument: --http-api-events
Environment Variable: WATCHTOWER_HTTP_API_EVENTS
                Type: Boolean
             Default: false
```

!!! Note
    Streams Watchtower operational events (scan started/completed, update started/completed/failed) via Server-Sent Events.

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/events/index.md) for details"

### HTTP API Health

Enables the health probe endpoints (`/livez`, `/readyz`, `/startupz`).

```text
            Argument: --http-api-health
Environment Variable: WATCHTOWER_HTTP_API_HEALTH
                Type: Boolean
             Default: false
```

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/health/index.md) for details"

### HTTP API History

Enables the scan history API endpoint (`/v1/history`).

```text
            Argument: --http-api-history
Environment Variable: WATCHTOWER_HTTP_API_HISTORY
                Type: Boolean
             Default: false
```

!!! Note
    Returns historical scan results from an in-memory ring buffer (up to 500 entries).

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/history/index.md) for details"

### HTTP API Images

Enables the images API endpoint (`/v1/images`).

```text
            Argument: --http-api-images
Environment Variable: WATCHTOWER_HTTP_API_IMAGES
                Type: Boolean
             Default: false
```

!!! Note
    Returns the current image identity and digest for every image tracked by Watchtower.

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/images/index.md) for details"

### HTTP API Metrics

Enables a Prometheus metrics endpoint via HTTP.

```text
            Argument: --http-api-metrics
Environment Variable: WATCHTOWER_HTTP_API_METRICS
                Type: Boolean
             Default: false
```

!!! Note "See the [Metrics API documentation](../../http-api/endpoints/metrics/index.md) for details"

### HTTP API Swagger

Enables the Swagger UI endpoint for interactive API documentation.

```text
            Argument: --http-api-swagger
Environment Variable: WATCHTOWER_HTTP_API_SWAGGER
                Type: Boolean
             Default: false
```

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/swagger/index.md) for details"

### HTTP API Update

Runs Watchtower in HTTP API mode, allowing updates only via HTTP requests.

```text
            Argument: --http-api-update
Environment Variable: WATCHTOWER_HTTP_API_UPDATE
                Type: Boolean
             Default: false
```

!!! Note
    Supports tag-specific filtering (e.g., `image=foo/bar:1.0`).

!!! Note "See the [HTTP API documentation](../../http-api/endpoints/update/index.md) for details"

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
