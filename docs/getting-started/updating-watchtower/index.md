# Updating Watchtower

If Watchtower is monitoring the same Docker daemon under which the Watchtower container itself is running (i.e. if you volume-mounted `/var/run/docker.sock` into the Watchtower container) then it has the ability to update itself.

If a new version of the `nickfedor/watchtower` image is pushed to the Docker Hub, your Watchtower will pull down the new image and restart itself automatically.

## Port Configuration Limitation

If a self-update is attempted when a port is mapped to a Watchtower container, then the new container will attempt to use the same port that is in use by the old container, which results in both containers being stopped.

When Watchtower has a port configured (e.g., via `--http-api-port` or Docker port mappings), self-updates are automatically skipped to prevent port conflicts.

To use the [HTTP API](../../advanced-features/http-api/index.md) or [Metrics API](../../advanced-features/metrics/index.md) with self-updates, consider one of the following approaches:

- **Remove port mappings**: Avoid publishing ports to the host and access the API through the Docker network instead.
- **Use `--run-once`**: Run Watchtower once without scheduling, then restart manually when needed.
- **Disable HTTP API**: Use only scheduled updates without the HTTP API if self-updates are required.
