# Health

## Overview

Watchtower provides three health probe endpoints that can be enabled using the [HTTP API Health](../../../configuration/http-api/index.md#http_api_health) configuration option.

## Endpoints

| **Name**  | **Method** | **Endpoint** | **Auth** |                          **Description**                          |
|:---------:|:----------:|:------------:|:--------:|:-----------------------------------------------------------------:|
| Liveness  |   `GET`    |   `/livez`   |    No    |            Returns `200 OK` when the server is running            |
| Readiness |   `GET`    |  `/readyz`   |    No    | Returns `200 OK` when Docker client is connected, `503` otherwise |
|  Startup  |   `GET`    | `/startupz`  |    No    |           Returns `200 OK` once the server has started            |
