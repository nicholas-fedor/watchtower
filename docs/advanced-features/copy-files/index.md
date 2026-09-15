# Copy Files

## Overview

When Watchtower recreates containers, it uses the Docker API data to ensure that items, such as bind mounts and volumes are preserved.
Files that exist only in the container's writable layer are not typically migrated to the new container.

As an example, [Docker Compose configs](https://docs.docker.com/reference/compose-file/configs/){target="_blank" rel="noopener noreferrer"} that are added to services using the `content:` or `environment:` option are written into that writable layer after a container is created.
The Docker Engine API does not record those paths on inspect, so Watchtower cannot discover them automatically.

In order to work around this limitation, add the [`com.centurylinklabs.watchtower.copy-file`](../../getting-started/container-selection/index.md#container_labels) label to the monitored container and use the in-container path of each file that should be copied onto the replacement container.

## Label

The label value is a comma-separated list of absolute paths inside the container.

=== "Docker Compose"

    ```yaml
    services:
        unbound:
            image: alpinelinux/unbound
            configs:
                - source: unbound_conf
                  target: /etc/unbound/unbound.conf
            labels:
                - "com.centurylinklabs.watchtower.copy-file=/etc/unbound/unbound.conf"
    ```

=== "Docker CLI"

    ```bash
    docker run -d \
        --label=com.centurylinklabs.watchtower.copy-file=/etc/unbound/unbound.conf \
        someimage
    ```

=== "Dockerfile"

    ```dockerfile
    LABEL com.centurylinklabs.watchtower.copy-file="/etc/unbound/unbound.conf"
    ```

Multiple files:

```yaml
labels:
    - "com.centurylinklabs.watchtower.copy-file=/etc/unbound/unbound.conf,/etc/nginx/nginx.conf"
```

## Compose Configs

| Compose source                                                                                                                                                                                                                           | How Docker Compose applies it          | Watchtower behavior                                                      |
|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------------------------------------|--------------------------------------------------------------------------|
| [`file:`](https://docs.docker.com/reference/compose-file/configs/#example-1){target="_blank" rel="noopener noreferrer"}                                                                                                                  | Bind-mount of the host file            | Preserved automatically. The copy-file label is not needed.              |
| [`content:`](https://docs.docker.com/reference/compose-file/configs/){target="_blank" rel="noopener noreferrer"} or [`environment:`](https://docs.docker.com/reference/compose-file/configs/){target="_blank" rel="noopener noreferrer"} | Copied into the container after create | Lost on recreate unless the copy-file label names the in-container path. |

Swarm [Docker configs](https://docs.docker.com/engine/swarm/configs/){target="_blank" rel="noopener noreferrer"} are a separate Engine feature for swarm services. This label is for standalone containers, including Compose on a non-swarm Docker Engine.

## Rules

- Paths must be absolute and must not contain `..`.
- Paths that are already bind mounts or volumes are skipped. Those mounts are already copied with the rest of the host config.
- A missing path is skipped.
- A directory, symlink, oversized file, unsafe archive, or copy failure stops that container's update.
- Files are not written into a container with a read-only root filesystem.

Each file is capped at 1 MiB. Watchtower keeps at most 16 MiB of copy-file data in memory at once.

## Underlying Technology

Watchtower uses the Docker Engine archive endpoints [`GET /containers/{id}/archive`](https://docs.docker.com/reference/api/engine/latest/#tag/Container/operation/ContainerArchive){target="_blank" rel="noopener noreferrer"} and [`PUT /containers/{id}/archive`](https://docs.docker.com/reference/api/engine/latest/#tag/Container/operation/PutContainerArchive){target="_blank" rel="noopener noreferrer"} to snapshot labeled files before the old container is removed and to write them into the new container before it starts.

That is the same mechanism [Docker Compose](https://docs.docker.com/reference/compose-file/configs/){target="_blank" rel="noopener noreferrer"} uses to inject `content:` and `environment:` configs.
