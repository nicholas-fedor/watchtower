# Web UI

## Overview

Watchtower includes a web UI served from the same port as the HTTP API. It lists the containers Watchtower is watching, checks them for newer images, updates the ones you choose, and shows recent scans and the active settings.

To enable it, include `ui` in [`http-api-endpoints`](../../../configuration/http-api/index.md#http_api_endpoints) and set an [`http-api-token`](../../../configuration/http-api/index.md#http_api_token). Then open `http://<host>:8080/`.

The UI has no data of its own. It reads the other HTTP API endpoints, so each part of the page only works when its endpoint is enabled:

| **Part of the page**   | **Endpoint needed** |
|:-----------------------|:-------------------:|
| Container list         |    `containers`     |
| Check for updates      |       `check`       |
| Update a container     |      `update`       |
| Scan history           |      `history`      |
| Settings               |      `config`       |

The simplest setup enables everything:

```yaml
services:
  watchtower:
    image: nickfedor/watchtower
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    ports:
      - 127.0.0.1:8080:8080
    environment:
      WATCHTOWER_HTTP_API_ENDPOINTS: all
      WATCHTOWER_HTTP_API_TOKEN: change-me
      WATCHTOWER_HTTP_API_EVENTS_TOKEN: change-me-too
      WATCHTOWER_HTTP_API_PERIODIC_POLLS: "true"
```

!!! Warning
    Enabling the `update` endpoint stops Watchtower's scheduled updates unless [`http-api-periodic-polls`](../../../configuration/scheduling/index.md#http_api_periodic_polls) is also set.

## Endpoints

| **Method** | **Endpoint**    | **Auth** | **Description**                                   |
|:----------:|:---------------:|:--------:|:--------------------------------------------------|
|   `GET`    | `/`, `/ui`      |    No    | Redirects to `/ui/`                               |
|   `GET`    | `/ui/`          |    No    | The page itself                                   |
|   `GET`    | `/ui/assets/*`  |    No    | Scripts, styles, and fonts embedded in the binary |
|   `GET`    | `/ui/session`   |    No    | Whether this browser is signed in                 |
|   `POST`   | `/ui/session`   |  Token   | Signs in with the HTTP API token                  |
|  `DELETE`  | `/ui/session`   |    No    | Signs out                                         |

## Signing In

The sign-in page asks for the [`http-api-token`](../../../configuration/http-api/index.md#http_api_token). When it matches, Watchtower sets an `access_token` cookie that the `/v1/*` endpoints accept in place of the `Authorization` header. The cookie is `HttpOnly` and `SameSite=Strict`, lasts 30 days, and is marked `Secure` when the page is served over HTTPS.

The page loads nothing from other hosts and is served with a `Content-Security-Policy` that only allows its own embedded files.

## Security

The same guidance that applies to the rest of the HTTP API applies here:

- Never expose the HTTP API (including the web UI) directly to the Internet.
- Always use TLS, either with [`http-api-tls-cert`](../../../configuration/http-api/index.md#http_api_tls_certificate) or a reverse proxy. Without it, the token and the session cookie cross the network in plain text.
- Anyone who signs in can update containers when the `update` endpoint is enabled. Leave `update` out of the list for a read-only page.

Page loads and API calls count toward [`http-api-rate-limit`](../../../configuration/http-api/index.md#http_api_rate_limit).
