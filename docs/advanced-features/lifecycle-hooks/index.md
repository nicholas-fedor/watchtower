# Lifecycle Hooks

## Executing commands before and after updating

!!! Important
    These are shell commands executed with `sh`, and therefore require the container to provide the `sh`
    executable.

!!! Note
    If the container is not running, lifecycle hooks (including pre-update hooks) cannot run, as the stop phase is skipped, and the update proceeds directly to removal (if applicable) or completion.

It is possible to execute _pre/post\-check_ and _pre/post\-update_ commands
**inside** every container updated by watchtower.

- The _pre-check_ command is executed for each container prior to every update cycle.
- The _pre-update_ command is executed before stopping the container when an update is about to start.
- The _post-update_ command is executed after restarting the updated container
- The _post-check_ command is executed for each container post every update cycle.

This feature is disabled by default. To enable it, you need to set the option
`--enable-lifecycle-hooks` on the command line, or set the environment variable
`WATCHTOWER_LIFECYCLE_HOOKS` to `true`.

### Specifying update commands

The commands are specified using docker container labels, the following are currently available:

| Type        | Docker Container Label                                 |
| ----------- | ------------------------------------------------------ |
| Pre Check   | `com.centurylinklabs.watchtower.lifecycle.pre-check`   |
| Pre Update  | `com.centurylinklabs.watchtower.lifecycle.pre-update`  |
| Post Update | `com.centurylinklabs.watchtower.lifecycle.post-update` |
| Post Check  | `com.centurylinklabs.watchtower.lifecycle.post-check`  |
| UID         | `com.centurylinklabs.watchtower.lifecycle.uid`         |
| GID         | `com.centurylinklabs.watchtower.lifecycle.gid`         |

### Specifying UID and GID for lifecycle hooks

By default, lifecycle hook commands run as the container's configured user (typically `root` if no `USER` directive is set). You can override this by specifying UID and GID using container labels or global flags.

!!! Note
    UID and GID values must be valid non-negative integers between 0 and 2,147,483,647 (2^31-1). Invalid values will be logged as warnings and ignored, falling back to the container's default user.

#### Container Labels

Use the following labels to specify UID and GID per container:

| Type | Docker Container Label                          |
| ---- | ----------------------------------------------- |
| UID  | `com.centurylinklabs.watchtower.lifecycle.uid`  |
| GID  | `com.centurylinklabs.watchtower.lifecycle.gid`  |

Example:

```dockerfile
LABEL com.centurylinklabs.watchtower.lifecycle.pre-update="/backup.sh"
LABEL com.centurylinklabs.watchtower.lifecycle.uid="1000"
LABEL com.centurylinklabs.watchtower.lifecycle.gid="1000"
```

#### Global Flags

Use the following flags to set default UID and GID for all lifecycle hooks:

- `--lifecycle-uid`: Default UID to run lifecycle hooks as
- `--lifecycle-gid`: Default GID to run lifecycle hooks as

Environment variables:

- `WATCHTOWER_LIFECYCLE_UID`
- `WATCHTOWER_LIFECYCLE_GID`

Container labels take precedence over global flags.

These labels can be declared as instructions in a Dockerfile (with some example .sh files) or be specified as part of
the `docker run` command line:

=== "Dockerfile"
    ```docker
    LABEL com.centurylinklabs.watchtower.lifecycle.pre-check="/sync.sh"
    LABEL com.centurylinklabs.watchtower.lifecycle.pre-update="/dump-data.sh"
    LABEL com.centurylinklabs.watchtower.lifecycle.post-update="/restore-data.sh"
    LABEL com.centurylinklabs.watchtower.lifecycle.post-check="/send-heartbeat.sh"
    LABEL com.centurylinklabs.watchtower.lifecycle.uid="1000"
    LABEL com.centurylinklabs.watchtower.lifecycle.gid="1000"
    ```

=== "docker run"
    ```bash
    docker run -d \
    --label=com.centurylinklabs.watchtower.lifecycle.pre-check="/sync.sh" \
    --label=com.centurylinklabs.watchtower.lifecycle.pre-update="/dump-data.sh" \
    --label=com.centurylinklabs.watchtower.lifecycle.post-update="/restore-data.sh" \
    --label=com.centurylinklabs.watchtower.lifecycle.post-check="/send-heartbeat.sh" \
    --label=com.centurylinklabs.watchtower.lifecycle.uid="1000" \
    --label=com.centurylinklabs.watchtower.lifecycle.gid="1000" \
    someimage
    ```

### Environment Variables

Lifecycle hook commands have access to container metadata through the `WT_CONTAINER` environment variable. This variable contains a JSON object with information about the container being updated:

```json
{
  "name": "my-container",
  "id": "abc123def456",
  "image_name": "nginx:latest",
  "stop_signal": "SIGTERM",
  "labels": {
    "com.centurylinklabs.watchtower.lifecycle.pre-update": "/custom-stop.sh"
  }
}
```

This allows scripts to access container-specific information for custom logic, such as implementing vendor-specific stop procedures.

#### Example: Custom stop command for Synology DSM

```bash
#!/bin/sh
# Parse container name from WT_CONTAINER
CONTAINER_NAME=$(echo $WT_CONTAINER | jq -r '.name')

# Use Synology API to stop container properly
synowebapi --exec api=SYNO.Docker.Container method="stop" name="$CONTAINER_NAME"
```

### Timeouts

The timeout for all lifecycle commands is 60 seconds. After that, a timeout will
occur, forcing Watchtower to continue the update loop.

#### Pre- or Post-update timeouts

For the `pre-update` or `post-update` lifecycle command, it is possible to override this timeout to
allow the script to finish before forcefully killing it. This is done by adding the
label `com.centurylinklabs.watchtower.lifecycle.pre-update-timeout` or post-update-timeout respectively followed by
the timeout expressed in minutes.

If the label value is explicitly set to `0`, the timeout will be disabled.

### Execution failure

The failure of a command to execute, identified by an exit code different than
0 or 75 (EX_TEMPFAIL), will not prevent watchtower from updating the container. Only an error
log statement containing the exit code will be reported.
