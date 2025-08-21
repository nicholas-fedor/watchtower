# HTTP API

## Overview

Watchtower has an [optional](../../configuration/arguments/index.md#http_api_mode) HTTP API server.

!!! Caution
    This is a relatively simple API with significant security implications.

## Endpoints

|            **Name**            | **Endpoint**  |          **Parameters**           |                                    **Description**                                    |
|:------------------------------:|:-------------:|:---------------------------------:|:-------------------------------------------------------------------------------------:|
|   [Update](#http_api_update)   | `/v1/update`  | [`image`](#image_parameter_usage) |         Triggers an update of containers monitored by the Watchtower instance         |
| [Metrics](../metrics/index.md) | `/v1/metrics` |                                   | Provides container scan and update information that's typically only seen via logging |

### HTTP API Update

To enable this mode, use the `--http-api-update` CLI argument or the `WATCHTOWER_HTTP_API_UPDATE` environment variable.

#### Requirements

##### Authentication

Watchtower uses token-based, header authentication for the HTTP API.

This should be set using the [HTTP API Token](../../configuration/arguments/index.md#http_api_token) configuration option.

All requests to the `/v1/update` endpoint will require a `Token` field in the request header with the predefined HTTP API token value.

##### Port Configuration

Watchtower defaults to using port 8080.
If port 8080 is used by another service, then it can be changed by using the [HTTP API Port](../../configuration/arguments/index.md#http_api_port) configuration option.

Alternatively, if Watchtower is being run via a Docker container, then the `host:container` port mapping can be updated accordingly (e.g. `8080:8080` -> `9000:8080`).

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
    command: --http-api-update
    environment:
      - WATCHTOWER_HTTP_API_TOKEN=mytoken
    labels:
      - "com.centurylinklabs.watchtower.enable=false"
    ports:
      - 8080:8080
    restart: unless-stopped
```
