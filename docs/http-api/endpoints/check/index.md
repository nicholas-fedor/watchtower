# Check

## Overview

The `v1/check` endpoint enables checking monitored containers for available image updates by querying the registry for the latest digest.

Include `check` in [`http-api-endpoints`](../../../configuration/http-api/index.md#http_api_endpoints) to enable this endpoint.

## Configuration

| Setting | Flag | Environment Variable | Default |
|:--------|:-----|:---------------------|:--------|
| Check API timeout | [`--http-api-check-timeout`](../../../configuration/http-api/index.md#http_api_check_timeout) | `WATCHTOWER_HTTP_API_CHECK_TIMEOUT` | `5m` |

## Parameters

### Image Name

The `image` parameter filters the check to only include containers running specific image names.

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/check?image=foo/bar:1.0"
```

### Container Name

The `container` parameter filters the check to only include specific containers by container name.

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/check?container=nginx"
```

### Timeout

The `timeout` parameter overrides the per-request timeout for this check.
It accepts Go durations such as `30s`, `2m`, or `5m`.
The value is capped by the configured check API timeout (`--http-api-check-timeout` / `WATCHTOWER_HTTP_API_CHECK_TIMEOUT`, default `5m`).

```bash
curl -X POST -H "Authorization: Bearer mytoken" "localhost:8080/v1/check?timeout=2m"
```

## Response Format

The `/v1/check` endpoint returns a JSON array of container check results:

```json
{
    "containers": [
        {
            "name": "nginx",
            "image": "nginx:latest",
            "has_update": true,
            "latest_image": "nginx:1.27"
        }
    ],
    "count": 1,
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `name`: Container name
- `image`: Current image reference with tag
- `has_update`: Whether a newer image is available
- `latest_image`: The newest available image reference (empty if no update available)

## HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | Check completed successfully                   |
|     401     | Invalid or missing authentication token        |
|     500     | Internal server error during request processing|
