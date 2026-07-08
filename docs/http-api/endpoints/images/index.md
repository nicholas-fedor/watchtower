# Images

## Overview

The `/v1/images/` endpoint lists the images tracked by Watchtower along with their current digests and container counts.
To enable this read-only endpoint, use the [HTTP API Images](../../../configuration/http-api/index.md#http_api_images) configuration option.

## Response Format

The `/v1/images` endpoint returns a JSON object with image statuses:

```json
{
    "images": [
        {
            "name": "nginx:latest",
            "image_id": "sha256:1111...",
            "digest": "sha256:2222...",
            "containers": 3
        }
    ],
    "count": 1,
    "timestamp": "2025-01-20T11:30:45Z",
    "api_version": "v1"
}
```

- `name`: Image name with tag
- `image_id`: Local image config ID (sha256:...)
- `digest`: Registry manifest digest (sha256:...). Empty for locally-built images.
- `containers`: Number of watched containers using this image

## Parameters

### Image Name

The `name` parameter filters results to a specific image by exact name.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/images?name=nginx:latest"
```

### Image ID

The `id` parameter filters results to a specific image by its image ID.

```bash
curl -H "Authorization: Bearer mytoken" "localhost:8080/v1/images?id=sha256:1111..."
```

## HTTP Status Codes

| Status Code | Description                                    |
|:-----------:|:-----------------------------------------------|
|     200     | Images retrieved successfully                  |
|     401     | Invalid or missing authentication token        |
|     500     | Internal server error during request processing|
