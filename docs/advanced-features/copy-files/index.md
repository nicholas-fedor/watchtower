# Copy Files

## Overview

When Watchtower recreates a container, it rebuilds that container from Docker API data. Bind mounts and volumes therefore carry over automatically.
Files that live only in the container's writable layer do not carry over.

[Docker Compose configs](https://docs.docker.com/reference/compose-file/configs/){target="_blank" rel="noopener noreferrer"} that use `content:` or `environment:` are written into that writable layer after the container is created, and the Engine does not record those paths on inspect.
Watchtower therefore cannot discover them on its own.

The [`com.centurylinklabs.watchtower.copy-file`](../../getting-started/container-selection/index.md#container_labels) label names the paths explicitly. Every named file is copied onto the replacement container.

## Setting the Label

The value is a comma-separated list of absolute paths inside the container.

=== "Docker Compose"

    ```yaml
    services:
        app:
            image: example/app:latest
            configs:
                - source: app_config
                  target: /etc/app/config.yml
            labels:
                - "com.centurylinklabs.watchtower.copy-file=/etc/app/config.yml"

    configs:
        app_config:
            content: |
                listen: 0.0.0.0:8080
    ```

=== "Docker CLI"

    ```bash
    docker run -d \
        --label=com.centurylinklabs.watchtower.copy-file=/etc/app/config.yml \
        example/app:latest
    ```

=== "Dockerfile"

    ```dockerfile
    LABEL com.centurylinklabs.watchtower.copy-file="/etc/app/config.yml"
    ```

### Several Files at Once

Separate the paths with commas:

```yaml
labels:
    - "com.centurylinklabs.watchtower.copy-file=/etc/app/config.yml,/etc/app/extra.conf"
```

## When the Label Is Required

### Compose Config Sources

| Compose source | How Compose applies it | Watchtower |
| --- | --- | --- |
| [`file:`](https://docs.docker.com/reference/compose-file/configs/#example-1){target="_blank" rel="noopener noreferrer"} | Bind-mount of the host file | Preserved automatically. The label is not needed. |
| [`content:` or `environment:`](https://docs.docker.com/reference/compose-file/configs/){target="_blank" rel="noopener noreferrer"} | Copied in after create | Lost on recreate, unless the label names the in-container path. |

!!! Note "Swarm configs are a separate feature"
    Swarm [Docker configs](https://docs.docker.com/engine/swarm/configs/){target="_blank" rel="noopener noreferrer"} are an Engine feature for swarm services. This label covers standalone containers, including Compose on a non-swarm Engine.

### Targets Inside a Mount Point

A config whose target sits inside a mount point needs the label as well.
This includes a path under a volume that the image declares with `VOLUME`.
Compose writes that file into the mount on every create, and Watchtower now does the same.

## Path Rules

Every labeled path ends in one of three outcomes: copied, skipped, or the update stops.

### Copied

| Condition | Result |
| --- | --- |
| Absolute path with no `..` | Copied onto the replacement container. |
| Parent directory missing from the new image | Watchtower creates it with mode `0755` and writes the file into it. |
| Container with a writable root filesystem | Watchtower copies the file, even when the path sits inside a mount. |

### Skipped

A skipped path is logged and does not block the update.

| Condition | Why |
| --- | --- |
| The path is itself a mount destination | That mount already provides the file, and it survives recreation. |
| The path is under a read-only mount | The Engine would refuse the write, and that mount provides the file regardless. |
| The path does not exist | There is nothing to copy. |
| The container has a read-only root filesystem | Watchtower extracts at the container root, where the Engine refuses any write. This applies even to a path under a writable volume. |

### Update Stops

| Condition | Why |
| --- | --- |
| The path is a directory | A directory cannot be copied as a file. |
| The path is a symlink | Symlinks are rejected rather than followed. |
| The file is larger than 1 MiB | The file exceeds the per-file cap. |
| The archive is unsafe | The archive contains traversal entries, extra members, or special file types. |
| The copy into the new container fails | The replacement container is discarded. |

A failure detected before the container is replaced leaves the running container untouched.

### Size Limits

Each file is capped at **1 MiB**.
Watchtower holds at most **16 MiB** of copy-file data in memory at once, across every container in a single update.

## Volumes

### Named Volumes

A [named volume](https://docs.docker.com/engine/storage/volumes/){target="_blank" rel="noopener noreferrer"} is preserved on recreation, so its contents survive an update on their own.

### Anonymous Volumes

Docker gives each container a fresh anonymous volume for every `VOLUME` an image declares. Watchtower therefore creates a new empty one on each update.
Only the files named by the copy-file label are written back into it. Everything else under that path is discarded.

The [`--remove-volumes`](../../configuration/update-behavior/index.md#remove_anonymous_volumes){target="_blank" rel="noopener noreferrer"} flag deletes the previous anonymous volume after an update, which also removes the labeled files it held.

## How It Works

### Archive Endpoints

Watchtower uses two Docker Engine archive endpoints: [`GET /containers/{id}/archive`](https://docs.docker.com/reference/api/engine/latest/#tag/Container/operation/ContainerArchive){target="_blank" rel="noopener noreferrer"} and [`PUT /containers/{id}/archive`](https://docs.docker.com/reference/api/engine/latest/#tag/Container/operation/PutContainerArchive){target="_blank" rel="noopener noreferrer"}.
The first endpoint snapshots labeled files before the old container is removed. The second endpoint writes them into the new container before it starts.

### Missing Parent Directories

That is the same mechanism [Docker Compose](https://docs.docker.com/reference/compose-file/configs/){target="_blank" rel="noopener noreferrer"} uses to inject `content:` and `environment:` configs.
Both extract the archive at the container root and name the single member with the full in-container path, so the Engine creates any parent directory the new image does not contain.
