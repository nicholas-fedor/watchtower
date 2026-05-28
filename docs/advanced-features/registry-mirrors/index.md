# Registry Mirrors

Registry mirrors provide alternative locations for pulling Docker images, useful when access to the default registry is slow, unreliable, or restricted by network policy. Watchtower uses these mirrors when checking whether containers need updates, ensuring it can detect new images even when the primary registry is unreachable.

## Overview

When the Docker daemon is configured with registry mirrors, Watchtower automatically detects and uses them during its update checks. This means:

- **Digest comparisons** — Watchtower fetches image manifests from mirrors before falling back to the canonical registry, so it can detect updates even when the primary registry is inaccessible.
- **All registries supported** — Mirrors can be configured globally (for all registries) or per-registry (e.g., a dedicated mirror for Docker Hub and a separate one for a private registry).

!!! Note
    Mirror support in Watchtower covers **digest comparison only** — determining whether a newer image exists. The actual image pull is handled by the Docker daemon, which already uses configured mirrors natively.

## Configuring Registry Mirrors

Registry mirrors are configured in the Docker daemon, not in Watchtower itself.

### Configuration File

- **Linux**: `/etc/docker/daemon.json`
- **Windows**: `C:\ProgramData\docker\config\daemon.json`
- **Rootless mode**: `~/.config/docker/daemon.json`

After making changes, restart the daemon:

```bash title="Linux with systemd"
sudo systemctl restart docker
```

### Global Mirrors

Global mirrors apply to all image registries. Add them under the `registry-mirrors` key:

```json title="/etc/docker/daemon.json"
{
    "registry-mirrors": [
        "https://mirror.example.com"
    ]
}
```

When multiple mirrors are listed, they are tried in order until one succeeds.

### Per-Registry Mirrors

For more granular control, configure mirrors for specific registries using the `registries` key. Each entry maps a registry name to its list of mirrors:

```json title="/etc/docker/daemon.json"
{
    "registries": {
        "docker.io": [
            "https://docker-hub-mirror.example.com"
        ],
        "registry.example.com": [
            "https://private-mirror.example.com"
        ]
    }
}
```

This is useful when different registries have different mirrors — for example, a local Harbor instance for Docker Hub and a separate Artifactory for a private registry.

### Command-Line Configuration

Global mirrors can also be set via the `dockerd` command line:

```bash
dockerd --registry-mirror https://mirror.example.com
```

!!! Note
    Per-registry mirrors can only be configured in `daemon.json`, not via command-line flags.

## How Watchtower Selects a Mirror

When checking if a container's image has been updated, Watchtower resolves the mirror to use in this order:

1. **Per-registry mirrors** — If the image's registry (e.g., `docker.io`) has dedicated mirrors configured, those are tried first.
2. **Global mirrors** — If no per-registry mirrors exist (or they all fail), the global mirror list is tried.
3. **Canonical registry** — If all mirrors fail, Watchtower falls back to the original registry (e.g., `index.docker.io`).

The first mirror to successfully respond with the image manifest wins. This means a fast, nearby mirror is preferred over a distant canonical registry.

## Configuration Examples

### Single Global Mirror

A single mirror used for all registries:

```json title="/etc/docker/daemon.json"
{
    "registry-mirrors": [
        "https://docker-mirror.company.com"
    ]
}
```

### Multiple Mirrors with Redundancy

Mirrors are tried in the order listed. If the first is unreachable, the next is used:

```json title="/etc/docker/daemon.json"
{
    "registry-mirrors": [
        "https://primary-mirror.company.com",
        "https://backup-mirror.company.com"
    ]
}
```

### Dedicated Mirrors per Registry

Different registries use different mirrors. This is useful in corporate environments where each registry has its own local cache:

```json title="/etc/docker/daemon.json"
{
    "registries": {
        "docker.io": [
            "https://docker-hub-mirror.company.com"
        ],
        "ghcr.io": [
            "https://ghcr-mirror.company.com"
        ],
        "registry.internal.company.com": [
            "https://harbor.internal.company.com"
        ]
    }
}
```

### Corporate Proxy Environment

When using internal mirrors that may use self-signed certificates:

```json title="/etc/docker/daemon.json"
{
    "registry-mirrors": [
        "https://harbor.internal.company.com"
    ],
  "insecure-registries": [
        "harbor.internal.company.com"
    ]
}
```

## Troubleshooting

### Mirrors Not Being Used

1. **Verify daemon configuration** — Confirm that `/etc/docker/daemon.json` contains valid JSON and the Docker daemon has been restarted since the last change.
2. **Enable debug logging** — Run Watchtower with debug logging to see mirror resolution in action:

    ```bash
    docker run -d --name watchtower \
    -e WATCHTOWER_DEBUG=true \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower
    ```

    Look for log output containing `Resolved registry mirror configuration`.
3. **Check network access** — Ensure the host can reach the mirror URLs (e.g., `curl -I https://mirror.example.com/v2/`).
4. **Verify mirror content** — Confirm the mirror is operational and has the required images.

### Authentication Issues

If a mirror requires authentication, configure credentials in the Docker daemon (e.g., `docker login`). Watchtower uses the same credentials for mirrors as for the canonical registry.

### SSL/TLS Errors

For mirrors using self-signed or internal CA certificates, add them to the `insecure-registries` list:

```json
{
  "insecure-registries": ["internal-mirror.company.com"]
}
```

!!! Warning
    Insecure registries accept HTTPS with untrusted certificates. Only use this for internal mirrors under your control.

## Related Features

- [Private Registries](../../configuration/private-registries/index.md) — Authenticating with private image registries
- [Secure Connections](../../configuration/secure-connections/index.md) — TLS and certificate configuration
