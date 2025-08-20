---
hide:
  - navigation
---
# Quickstart

Despite offering extensive configuration options, Watchtower's default settings are suitable for most deployments.
If you need to modify the configuration, then review the available [documentation](../configuration/arguments/index.md).

## Docker CLI

[Docker Run CLI Reference](https://docs.docker.com/reference/cli/docker/container/run/){target="_blank" rel="noopener noreferrer"}

```bash title="Pull and run Watchtower"
docker run -d \
--name watchtower \
--restart unless-stopped \
-v /var/run/docker.sock:/var/run/docker.sock \
nickfedor/watchtower
```

## Docker Compose

[Docker Compose File Reference](https://docs.docker.com/reference/compose-file/){target="_blank" rel="noopener noreferrer"}

1) Download or copy the example Docker Compose file:
<!-- markdownlint-disable -->
=== "Windows"

    ```powershell
    iwr -Uri https://raw.githubusercontent.com/nicholas-fedor/watchtower/refs/heads/main/examples/default/docker-compose.yaml -OutFile docker-compose.yaml
    ```

=== "Linux"

    ```bash
    curl -L https://raw.githubusercontent.com/nicholas-fedor/watchtower/refs/heads/main/examples/default/docker-compose.yaml -o docker-compose.yaml
    ```

```yaml title="docker-compose.yaml"
services:
  watchtower:
    image: nickfedor/watchtower:latest
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
```

2) Run the Compose file:
<!-- markdownlint-restore -->

  ```bash
  docker compose up -d
  ```

## Expected Behavior

When running Watchtower with its default settings:

- It will monitor all running containers on the host
- Every 24 hours, it will poll if the monitored containers have updated image digests

If an updated image digest is detected, then Watchtower will:

- Pull the updated container image
- Perform a graceful shutdown of the target container and its dependencies
- Start a new container with the updated image while maintaining the previous container's configuration
