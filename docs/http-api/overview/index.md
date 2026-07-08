# API

## Overview

Watchtower has an optional HTTP API server that can be enabled by using the [HTTP API configuration options](../../configuration/http-api/index.md).

## Security Considerations

Watchtower was originally designed to be a stateless application with direct access to the Docker socket.
By having direct access to the Docker socket, the application effectively runs with root-level privileges.
It is worth taking a moment to review and familiarize yourself with the [Docker Engine security documentation](https://docs.docker.com/engine/security/){target="_blank" rel="noopener noreferrer"}.

- !!! Warning "The HTTP API should never be directly exposed to the Internet!"
- !!! Warning "Using the HTTP API without TLS encryption is insecure and not recommended!"
- !!! Warning "Only enable the endpoints that you need!"

## Endpoints

|                           **Name**                           | **Method** |       **Endpoint**       | **Auth** |                                                                                **Parameters**                                                                                |                                              **Description**                                              |
|:------------------------------------------------------------:|:----------:|:------------------------:|:--------:|:----------------------------------------------------------------------------------------------------------------------------------------------------------------------------:|:---------------------------------------------------------------------------------------------------------:|
|            [Update](../endpoints/update/index.md)            |   `POST`   |       `/v1/update`       |   Yes    | [`image`](../endpoints/update/index.md#image_name), [`container`](../endpoints/update/index.md#container_name), [`async`](../endpoints/update/index.md#asynchronous_updates) |                   Triggers container updates and returns JSON results of the operation                    |
|             [Check](../endpoints/check/index.md)             |   `POST`   |       `/v1/check`        |   Yes    |                                 [`image`](../endpoints/check/index.md#image_name), [`container`](../endpoints/check/index.md#container_name)                                 |                     Checks containers for available updates via registry digest query                     |
|        [Containers](../endpoints/containers/index.md)        |   `GET`    |     `/v1/containers`     |   Yes    |                              [`name`](../endpoints/containers/index.md#container_name), [`image`](../endpoints/containers/index.md#image_name)                               |                     Lists watched containers and their current running image digests                      |
| [Container Details](../endpoints/container-details/index.md) |   `GET`    | `/v1/containers/details` |   Yes    |                       [`name`](../endpoints/container-details/index.md#container_name), [`image`](../endpoints/container-details/index.md#image_name)                        | Returns detailed information about each watched container including running state and configuration flags |
|           [History](../endpoints/history/index.md)           |   `GET`    |      `/v1/history`       |   Yes    |                [`since`](../endpoints/history/index.md#since), [`until`](../endpoints/history/index.md#until), [`limit`](../endpoints/history/index.md#limit)                |            Returns historical scan results from the in-memory ring buffer (up to 500 entries)             |
|            [Images](../endpoints/images/index.md)            |   `GET`    |       `/v1/images`       |   Yes    |                                       [`name`](../endpoints/images/index.md#image_name), [`id`](../endpoints/images/index.md#image_id)                                       |                   Lists tracked images with their current digests and container counts                    |
|            [Config](../endpoints/config/index.md)            |   `GET`    |       `/v1/config`       |   Yes    |                                                                                                                                                                              |                           Returns the active Watchtower configuration settings                            |
|            [Events](../endpoints/events/index.md)            |   `GET`    |       `/v1/events`       |   Yes    |                                                                                                                                                                              |                        Streams real-time operational events via Server-Sent Events                        |
|            [Status](../endpoints/status/index.md)            |   `GET`    |       `/v1/status`       |   Yes    |                                                                                                                                                                              |                                Returns the summary of the most recent scan                                |
|           [Metrics](../endpoints/metrics/index.md)           |   `GET`    |      `/v1/metrics`       |   Yes    |                                                                                                                                                                              |                     Exposes Prometheus-compatible metrics for monitoring and alerting                     |
|           [Swagger](../endpoints/swagger/index.md)           |   `GET`    |       `/swagger/*`       |    No    |                                                                                                                                                                              |                               Interactive API documentation via Swagger UI                                |
|                           Liveness                           |   `GET`    |         `/livez`         |    No    |                                                                                                                                                                              |                                Returns `200 OK` when the server is running                                |
|                          Readiness                           |   `GET`    |        `/readyz`         |    No    |                                                                                                                                                                              |                     Returns `200 OK` when Docker client is connected, `503` otherwise                     |
|                           Startup                            |   `GET`    |       `/startupz`        |    No    |                                                                                                                                                                              |                               Returns `200 OK` once the server has started                                |

!!! Note
    - Endpoints enforce HTTP method restrictions using method-based routing.
    - Requests with unsupported methods will receive a `405 Method Not Allowed` response.
