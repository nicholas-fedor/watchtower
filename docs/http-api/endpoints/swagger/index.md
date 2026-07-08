# Swagger UI

## Overview

The `/swagger/` endpoint serves an interactive, browser-based Swagger UI that renders the Watchtower HTTP API specification.

To enable the Swagger UI, use the [HTTP API Swagger](../../../configuration/http-api/index.md#http_api_swagger) configuration option or enable all endpoints at once with the [`http-api-full`](../../../configuration/http-api/index.md#http_api_full) configuration option.

## Endpoint

| **Name**  | **Method** | **Endpoint** | **Auth** | **Description**                                                |
|:---------:|:----------:|:------------:|:--------:|:---------------------------------------------------------------|
| Swagger   |   `GET`    | `/swagger/*` |    No    | Interactive API documentation rendered via Swagger UI          |

The endpoint is mounted as a wildcard route (`/swagger/`) so that all Swagger UI assets are served under this prefix.
Accessing `/swagger/index.html` returns the main documentation page and sub-paths serve the static assets the UI needs.

## Security

The Swagger UI endpoint does not require authentication.
The UI must be accessible to browsers without a Bearer token in order to render, and the
request-building it enables is only useful if the caller can already authenticate to the protected `/v1/*` endpoints separately.

That said, the same security guidance that applies to the rest of the HTTP API applies
here as well:

- Never expose the HTTP API (including the Swagger UI) directly to the Internet.
- Always use TLS.
- Only enable the endpoints you actually need.

If you place the API behind a reverse proxy (Traefik, Caddy, Nginx, etc.), configure [trusted proxies](../../../configuration/http-api/index.md#http_api_trusted_proxies) and [CORS origins](../../../configuration/http-api/index.md#http_api_cors_origins) accordingly.

## OpenAPI Specification

The UI is powered by a machine-readable [OpenAPI Specification](https://spec.openapis.org/){target="_blank" rel="noopener noreferrer"} (OAS) document that describes every enabled Watchtower endpoint.
This project uses the **Swagger 2.0** format (OAS 2.0), rendered as `swagger.json` and `swagger.yaml`.

!!! Note
    Swagger UI renders the spec as CommonMark in most tooltip and description fields, per the [OAS rich text requirements](https://spec.openapis.org/#rich-text-formatting){target="_blank" rel="noopener noreferrer"}.

## Under the Hood

The Swagger 2.0 specification is generated from annotions in the codebase using [`swaggo/swag`](https://github.com/swaggo/swag){target="_blank" rel="noopener noreferrer"}.

Then, Fiber v3's [`gofiber/contrib/v3/swaggo`](https://docs.gofiber.io/contrib/v3_swaggo_v1.x.x/swaggo/){target="_blank" rel="noopener noreferrer"} middleware package is used to serve the bundled [Swagger UI](https://swagger.io/docs/open-source-tools/swagger-ui/){target="_blank" rel="noopener noreferrer"} assets and generated specification.

The generated files (`docs.go`, `swagger.json`, `swagger.yaml`) can be found in the repository at  [https://github.com/nicholas-fedor/watchtower/tree/main/internal/api/swagger](https://github.com/nicholas-fedor/watchtower/tree/main/internal/api/swagger){target="_blank" rel="noopener noreferrer"}.

## Using the Swagger UI

### Starting the Server

Enable the endpoint and start Watchtower:

=== "Without TLS"

    === "Docker Compose"

        ```yaml
        services:
            watchtower:
                image: nickfedor/watchtower:latest
                volumes:
                    - /var/run/docker.sock:/var/run/docker.sock
                environment:
                    - WATCHTOWER_HTTP_API_SWAGGER=true
                ports:
                    - "8080:8080"
                restart: unless-stopped
        ```

    === "Docker CLI"

        ```bash
        docker run -d \
            --name watchtower \
            -v /var/run/docker.sock:/var/run/docker.sock \
            -e WATCHTOWER_HTTP_API_SWAGGER=true \
            -p 8080:8080 \
            --restart unless-stopped \
            nickfedor/watchtower
        ```

    Then open the Swagger UI in a web browser:

    ```url
    http://<host>:8080/swagger/index.html
    ```

=== "With TLS"

    === "Docker Compose"

        ```yaml
        services:
            watchtower:
                image: nickfedor/watchtower:latest
                volumes:
                    - /var/run/docker.sock:/var/run/docker.sock
                    - /opt/watchtower/certs:/certs:ro
                environment:
                    - WATCHTOWER_HTTP_API_SWAGGER=true
                    - WATCHTOWER_HTTP_API_TLS_CERT=/certs/watchtower.crt
                    - WATCHTOWER_HTTP_API_TLS_KEY=/certs/watchtower.key
                ports:
                    - "8080:8080"
                restart: unless-stopped
        ```

    === "Docker CLI"

        ```bash
        docker run -d \
            --name watchtower \
            -v /var/run/docker.sock:/var/run/docker.sock \
            -v /opt/watchtower/certs:/certs:ro \
            -e WATCHTOWER_HTTP_API_SWAGGER=true \
            -e WATCHTOWER_HTTP_API_TLS_CERT=/certs/watchtower.crt \
            -e WATCHTOWER_HTTP_API_TLS_KEY=/certs/watchtower.key \
            -p 8080:8080 \
            --restart unless-stopped \
            nickfedor/watchtower
        ```

    Then open the Swagger UI in a web browser:

    ```url
    https://<host>:8080/swagger/index.html
    ```

### Interacting with Authenticated Endpoints

Swagger UI supports an [API Key](https://swagger.io/docs/specification/authentication/api-keys/){target="_blank" rel="noopener noreferrer"} security scheme named `BearerAuth`.

To authenticate:

1. Click the **Authorize** button at the top-right of the page.
2. Enter the token you configured via `--http-api-token` (or the `WATCHTOWER_HTTP_API_TOKEN` environment variable).
3. Click **Authorize**, then **Close**.

The token is sent with every subsequent request as a `Bearer` token in the `Authorization` header, matching the scheme required by the protected `/v1/*` endpoints.

For the events SSE stream (`/v1/events`), supply the events token you configured via `--http-api-events-token`.
The UI sends this as a standard `Authorization: Bearer` header, which the endpoint accepts.

### Query parameters

Swagger UI exposes any `@Param` query parameters documented in the spec.
For example, the `/v1/update` endpoint accepts the following documented query parameters:

| Parameter   | Type      | Required | Description                                                        |
|-------------|-----------|----------|--------------------------------------------------------------------|
| `image`     | `string`  | No       | Comma-separated image names or Go regex patterns to filter.        |
| `container` | `string`  | No       | Comma-separated container name patterns to filter.                 |
| `async`     | `boolean` | No       | Run the update asynchronously; returns `202 Accepted` when `true`. |

### `Try it out`

Swagger UI's **Try it out** button issues a real HTTP request from your browser directly to the Watchtower server.
Because Swagger UI is served from the same origin, no CORS preflight is required when both are accessed from the same host and port (the default setup).
When the UI is served from a different origin, ensure the `--http-api-cors-origins` setting allows the browser origin.

!!! Note
    The "Try it out" feature uses the browser's `fetch` API.
    Requests to the [`/v1/events`](../../endpoints/events/index.md) SSE stream will not stream correctly through Swagger UI because browsers cannot reuse `fetch` connections as `EventSource`.
    Use a dedicated SSE client or `curl` for event streams.

## Troubleshooting

The `/swagger/*` endpoint and Swagger UI return standard HTTP status codes.

Common issues and their resolutions:

| HTTP Status | Cause                                                                 | Resolution                                                                                                                          |
|:-----------:|:----------------------------------------------------------------------|:------------------------------------------------------------------------------------------------------------------------------------|
|    `404`    | The endpoint may not be enabled                                       | Enable the endpoint using the [`http-api-swagger`](../../../configuration/http-api/index.md#http_api_swagger) configuration option. |
|    `404`    | Requested a path outside `/swagger/*`, e.g. `/swagger` (no wildcard). | Navigate to `/swagger/index.html`.                                                                                                  |
|    `405`    | HTTP method other than `GET` used against `/swagger/*`.               | Use `GET` only. `POST`, `PUT`, and `DELETE` are not supported on this path.                                                         |

The server startup log confirms successful registration:

```text
2026/07/06 20:00:00 [INFO]  HTTP API listening on :8080
```

## References

- [OpenAPI Specification 3.1.1](https://spec.openapis.org/){target="_blank" rel="noopener noreferrer"} — the normative specification that defines the structure OAS documents (Swagger 2.0 is a prior version of the same standard; see [OAS Learn](https://learn.openapis.org/))
- [Swagger UI — official tooling](https://swagger.io/tools/swagger-ui/){target="_blank" rel="noopener noreferrer"} — the open-source UI that renders the interactive documentation
- [`gofiber/contrib/v3/swaggo`](https://docs.gofiber.io/contrib/v3_swaggo_v1.x.x/swaggo/){target="_blank" rel="noopener noreferrer"} — Fiber v3 integration middleware that serves the UI and spec
- [`swaggo/swag`](https://github.com/swaggo/swag){target="_blank" rel="noopener noreferrer"} — the Go annotation parser and code generator that produces the OpenAPI 2.0 spec from handler comments
