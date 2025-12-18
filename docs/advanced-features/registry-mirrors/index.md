# Registry Mirrors

Registry mirrors allow Docker to pull images from alternative registries instead of the official Docker Hub or other configured registries.
This feature is particularly useful for improving performance, reducing bandwidth usage, and ensuring compliance with local regulations or network policies.

## What are Registry Mirrors?

Registry mirrors are alternative locations that host the same images as the official registries.
When Docker pulls an image, it can check these mirrors first before falling back to the original registry. This provides several benefits:

- **Performance**: Mirrors located closer to your infrastructure can significantly reduce pull times
- **Bandwidth Savings**: Reduce internet bandwidth usage by serving images from local or regional mirrors
- **Reliability**: Provide redundancy if the primary registry is unavailable
- **Compliance**: Meet regulatory requirements by keeping image pulls within local networks
- **Cost Control**: Reduce data transfer costs when using cloud providers

## Docker Daemon Configuration

Registry mirrors are configured at the Docker daemon level, not in individual containers or applications. This ensures that all image pulls on the host use the configured mirrors.

### Using daemon.json

The preferred method is to configure mirrors in the Docker daemon configuration file:

```json
{
  "registry-mirrors": [
    "https://mirror.example.com",
    "https://registry-mirror.company.com:5000"
  ]
}
```

- **Linux**: `/etc/docker/daemon.json`
- **Windows**: `C:\ProgramData\docker\config\daemon.json`
- **Rootless mode**: `~/.config/docker/daemon.json`

After modifying the configuration file, restart the Docker daemon:

```bash
# Linux with systemd
sudo systemctl restart docker

# Or reload without stopping containers
sudo systemctl reload docker
```

### Using Command Line Flags

You can also configure mirrors when starting the daemon manually:

```bash
dockerd --registry-mirror https://mirror.example.com --registry-mirror https://registry-mirror.company.com:5000
```

### Per-Registry Mirrors

Docker also supports configuring mirrors for specific registries:

```json
{
  "registry-mirrors": [
    "https://global-mirror.example.com"
  ],
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

!!! Note
    Per-registry mirror configuration may require specific Docker versions and registry setups.
    Global mirrors are more commonly used and widely supported.

## Watchtower's Mirror Support

Watchtower automatically detects and utilizes registry mirrors configured in your Docker daemon.
When updating containers, Watchtower will attempt to pull newer images using the same mirror configuration that Docker uses.

### Mirror Selection Logic

Watchtower follows this mirror selection process when pulling images:

1. **Check for Per-Registry Mirrors**: If per-registry mirrors are configured for the target registry, they are tried first
2. **Fall Back to Global Mirrors**: If no per-registry mirrors are found or they fail, global mirrors are attempted in the order they are configured
3. **Original Registry**: If all mirrors fail, Watchtower falls back to pulling directly from the original registry

### Logging

Watchtower logs mirror usage during the update process:

```text
INFO[0001] Registry Mirrors: https://mirror.example.com, https://registry-mirror.company.com:5000
INFO[0002] Successfully pulled image from mirror        container=web-app image=nginx:latest mirror=https://mirror.example.com
```

If a mirror fails, Watchtower will log the failure and try the next mirror:

```text
DEBUG[0002] Failed to pull from mirror, trying next      container=web-app image=nginx:latest mirror=https://mirror.example.com
```

### Mirror Host Detection

Watchtower intelligently handles image references to avoid infinite loops when mirrors redirect to other mirrors.
It checks if the target registry is already a configured mirror before attempting to rewrite the image reference.

## Configuration Examples

### Basic Global Mirror Setup

For a single mirror serving all registries:

```json title="/etc/docker/daemon.json"
{
  "registry-mirrors": [
    "https://docker-mirror.company.com"
  ]
}
```

### Multiple Mirrors with Fallback

Configure multiple mirrors for redundancy:

```json title="/etc/docker/daemon.json"
{
  "registry-mirrors": [
    "https://primary-mirror.company.com",
    "https://backup-mirror.company.com",
    "https://registry-1.docker.io"
  ]
}
```

### Corporate Environment Setup

In corporate environments with local mirrors:

```json title="/etc/docker/daemon.json"
{
  "registry-mirrors": [
    "https://harbor.internal.company.com",
    "https://artifactory.company.com/artifactory/api/docker/docker-hub"
  ],
  "insecure-registries": [
    "harbor.internal.company.com",
    "artifactory.company.com"
  ]
}
```

### Cloud Provider Mirrors

Using cloud provider mirror services:

```json title="/etc/docker/daemon.json"
{
  "registry-mirrors": [
    "https://mirror.gcr.io",
    "https://registry-1.docker.io"
  ]
}
```

## Troubleshooting

### Mirrors Not Being Used

- **Verify Configuration**: Ensure the daemon.json file is valid JSON and the daemon has been restarted
- **Check Logs**: Watchtower logs detected mirrors at startup. Look for "Registry Mirrors:" in the logs
- **Network Connectivity**: Ensure the host can reach the mirror URLs
- **Mirror Availability**: Verify that the mirror is responding and has the required images

### Authentication Issues

Some mirrors may require authentication. Ensure your Docker daemon is configured with appropriate credentials if needed.

### SSL/TLS Issues

If mirrors use self-signed certificates, you may need to configure insecure registries:

```json
{
  "registry-mirrors": ["https://internal-mirror.company.com"],
  "insecure-registries": ["internal-mirror.company.com"]
}
```

### Performance Issues

- **Mirror Order**: Place faster, more reliable mirrors first in the list
- **Mirror Health**: Monitor mirror performance and remove slow or unreliable ones
- **Local Caching**: Consider running a local registry mirror for better performance

## Best Practices

### Mirror Selection

- Choose mirrors geographically close to your infrastructure
- Prefer mirrors with good uptime and performance
- Use multiple mirrors for redundancy
- Regularly test mirror availability

### Security Considerations

- Use HTTPS mirrors when possible
- Be aware that mirrors may cache malicious images
- Monitor mirror logs for suspicious activity
- Keep mirror software updated

### Monitoring

- Monitor Docker daemon logs for mirror usage
- Track image pull performance with and without mirrors
- Set up alerts for mirror failures
- Regularly audit mirror configurations

## References

- [Docker Registry Mirrors](https://docs.docker.com/engine/daemon/#mirrors) - Official Docker daemon mirror configuration
- [Docker Registry Recipes - Mirror](https://docs.docker.com/registry/recipes/mirror/) - Running a registry as a pull-through cache
- [Docker Daemon Configuration](https://docs.docker.com/engine/reference/commandline/dockerd/) - Complete dockerd options reference
- [Registry Access Management](https://docs.docker.com/docker-hub/access-tokens/) - Docker Hub access control

## Related Features

- [Private Registries](../../configuration/private-registries/index.md) - Authentication for private registries
- [Secure Connections](../../configuration/secure-connections/index.md) - TLS configuration options
- [HTTP API](../http-api/index.md) - Watchtower's HTTP API for monitoring
