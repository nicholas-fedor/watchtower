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
- **Ephemeral Self-Updates**: Enable Watchtower to use a separate, short-lived container to orchestrate the self-update process.

## Ephemeral Self-Update

!!! Warning "This is an experimental feature"

The ephemeral self-update mechanism is an alternative to the default rename-based approach. It uses a short-lived orchestrator container to perform the container replacement, providing a more atomic handoff between old and new Watchtower instances.

### How It Works

1. Watchtower detects a new version of its own image is available and pulls it.
2. A short-lived orchestrator container is created from the new Watchtower image with the `--self-update-orchestrator` internal flag.
3. The orchestrator mounts the Docker socket and performs the following sequence:
    - Stops the old Watchtower container.
    - Creates a new container from the new image with the same configuration.
    - Starts the new Watchtower container.
    - Verifies the new container is running.
    - Removes the old container.
4. The orchestrator exits and is automatically removed (via Docker's `AutoRemove`).

### Enabling Ephemeral Self-Updates

```bash
docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    --restart unless-stopped \
    -e WATCHTOWER_EPHEMERAL_SELF_UPDATE=true \
    nickfedor/watchtower
```

### Differences from Default Self-Update

| Aspect                | Default (Rename)                   | Ephemeral                                |
|-----------------------|------------------------------------|------------------------------------------|
| Mechanism             | Renames old container, creates new | Orchestrator handles stop/create/start   |
| Port conflicts        | Skipped automatically              | Orchestrator has no ports                |
| Old container cleanup | Deferred to next startup           | Immediate removal by orchestrator        |
| Failure recovery      | Old container persists (renamed)   | Old container preserved if new one fails |

### Limitations

- The Docker socket must be mounted in the Watchtower container (required for both mechanisms).
- The orchestrator container is identified by the `com.centurylinklabs.watchtower.ephemeral-orchestrator` label. Orphaned orchestrators from crashes are cleaned up on Watchtower startup.
