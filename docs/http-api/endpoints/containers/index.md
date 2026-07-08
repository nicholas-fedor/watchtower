# Containers

## Overview

The `/v1/containers` endpoint lists monitored containers along with their current image ID.

To enable this read-only endpoint, use the [HTTP API Containers](../../../configuration/http-api/index.md#http_api_containers) configuration option.

## Parameters

### Container Name

The `name` parameter filters results to a specific container by exact name.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/containers?name=nginx"
```

### Image Name

The `image` parameter filters results to containers running a specific image.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/containers?image=nginx:latest"
```

## Response Format

The `/v1/containers` endpoint returns a JSON array of watched containers:

```json
{
    "containers": [
        {
            "name": "nginx",
            "image": "nginx:latest",
            "image_id": "sha256:1111...",
            "digest": "sha256:2222..."
        }
    ],
    "count": 1,
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `name`: Container name
- `image`: Image reference with tag
- `image_id`: Local image config ID
- `digest`: Registry manifest digest the image was pulled from (from the image's `RepoDigests`), directly comparable to a registry's `Docker-Content-Digest`. Empty for locally-built images with no registry reference.

!!! Note
    [HTTP API Containers](../../../configuration/http-api/index.md#http_api_containers) can be enabled alongside [HTTP API Update](../../../configuration/http-api/index.md#http_api_update) and [HTTP API Metrics](../../../configuration/http-api/index.md#http_api_metrics).
