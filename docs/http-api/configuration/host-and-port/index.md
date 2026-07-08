# Configuration

## Overview

Watchtower defaults to listening on all interfaces on port 8080.
This behavior can be changed by configuring the [HTTP API Host](../../../configuration/http-api/index.md#http_api_host) and [HTTP API Port](../../../configuration/http-api/index.md#http_api_port) configuration options

## Configuration Options

### HTTP API Host

Use the [HTTP API Host](../../../configuration/http-api/index.md#http_api_host) configuration option to bind to a specific host interface.

- This must be a valid IP address (IPv4 or IPv6).
- If not specified, Watchtower listens on all interfaces on the port specified by [HTTP API Port](../../../configuration/http-api/index.md#http_api_port).

### HTTP API Port

The port can be changed using the [HTTP API Port](../../../configuration/http-api/index.md#http_api_port) configuration option.

If Watchtower is being run via a Docker container, then the `host:container` port mapping can be updated accordingly (e.g. `8080:8080` -> `9000:8080`).

## Examples

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
