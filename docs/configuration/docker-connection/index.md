# Docker Connection

## Docker Host

Specifies the Docker daemon socket to connect to, supporting remote hosts via TCP (e.g., `tcp://hostname:port`).

```text
            Argument: --host, -H
Environment Variable: DOCKER_HOST
                Type: String
             Default: unix:///var/run/docker.sock
```

## Docker API Version

Sets the Docker API version for client-daemon communication.

```text
            Argument: --api-version, -a
Environment Variable: DOCKER_API_VERSION
                Type: String
             Default: Autonegotiated
```

!!! Note
    Falls back to autonegotiation on failure.

!!! Warning
    Refer to Docker's [API version matrix](https://docs.docker.com/reference/api/engine/#api-version-matrix){target="_blank" rel="noopener noreferrer"} for compatibility.

## Enable Docker TLS Verification

Enables TLS verification for Docker socket connections.

```text
            Argument: --tlsverify
Environment Variable: DOCKER_TLS_VERIFY
                Type: Boolean
             Default: false
```

## TLS Certificates Path

Path to TLS certificates for remote Docker connections.

```text
            Argument: --cert-path
Environment Variable: DOCKER_CERT_PATH
                Type: String
             Default: None
```

## Disable Memory Swappiness

Sets memory swappiness to `nil` for Podman compatibility with crun and cgroupv2, overriding Podman's default of `0`.

```text
            Argument: --disable-memory-swappiness
Environment Variable: WATCHTOWER_DISABLE_MEMORY_SWAPPINESS
                Type: Boolean
             Default: false
```

## CPU Copy Mode

Controls how CPU settings are copied when recreating containers, addressing Podman compatibility issues with CPU limits.
Podman handles NanoCPUs differently than Docker, which can cause container recreation failures.

```text
            Argument: --cpu-copy-mode
Environment Variable: WATCHTOWER_CPU_COPY_MODE
                Type: String
     Possible Values: auto, full, none
             Default: auto
```

!!! Note
    - **auto**: Automatically detects if running on Podman and filters NanoCPUs for compatibility. On Docker, copies all CPU settings.
    - **full**: Copies all CPU settings unchanged (original behavior).
    - **none**: Strips all CPU limits to avoid compatibility issues.

Use `auto` in mixed Docker/Podman environments.
Use `full` if running only on Docker and want to preserve all CPU limits.
Use `none` if CPU limits are causing issues and you prefer no limits on recreated containers.

### Usage Examples

Run Watchtower with automatic CPU compatibility:

```bash
docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    --cpu-copy-mode auto
```

Force full CPU copying (Docker-only environments):

```bash
docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    --cpu-copy-mode full
```

Strip all CPU limits:

```bash
docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    nickfedor/watchtower \
    --cpu-copy-mode none
```

## Recreating Containers on Podman

Watchtower talks to Podman through its Docker-compatible API. It recreates a container from what that API reports. A setting the API does not carry is lost on recreation.

- **Timezone and umask**: Podman's `--tz` and `--umask` exist only in Podman's own API. A recreated container uses the image's timezone. Its processes run with the OCI runtime's default umask, normally `0022`.
- **Log driver**: the Docker-compatible API reports a `k8s-file` log driver as `json-file` and leaves out its options, such as `max-size`. A recreated container uses `json-file` without those options. Its log size limit falls back to `log_size_max` in `containers.conf`, which is unlimited by default.
- **Podman metadata**: fields such as `CreateCommand` and the systemd notify mode are not part of the Docker API. A recreated container does not have them.

To keep these settings, set them where Watchtower does not need to copy them. For example, set the timezone through the `TZ` environment variable. Watchtower keeps the variable on recreation. It takes effect when the image or application reads it. Or manage the container with a Podman Quadlet or systemd unit and let Podman's own auto-update restart it.
