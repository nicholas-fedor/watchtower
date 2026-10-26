# Running Multiple Instances

By default, Watchtower stops and removes the other Watchtower instances it finds on its Docker host, so only one instance runs. To run several instances on one host, give each one its own scope. Each instance then:

- Manages only the containers in its scope
- Removes only the other instances in its scope

!!! Note
    Two instances can't share a scope. When an instance starts, it removes the other instances in its scope.

## Scoped Instances

### Defining a Scope

An instance takes its scope from one of these:

- Its [`scope`](../../configuration/container-selection/index.md#scope_filter) configuration option
- The `com.centurylinklabs.watchtower.scope` label of its own container, when that option is not set

The second source is described in [Scope From the Container Label](#scope_from_the_container_label).

The instance then manages each container whose `com.centurylinklabs.watchtower.scope` label names that scope, including its own Watchtower container.

!!! Warning
    Without the scope label on its own container, a scoped instance counts as unscoped to other instances. An unscoped instance on the same host then removes it at startup.

### Example: One Scope

In this Docker Compose file, `watchtower` manages `app` and itself:

```yaml
services:
    app:
        image: myapps/app
        labels:
            - "com.centurylinklabs.watchtower.scope=myscope"

    watchtower:
        image: nickfedor/watchtower
        volumes:
            - "/var/run/docker.sock:/var/run/docker.sock"
        environment:
            - WATCHTOWER_SCOPE=myscope
        labels:
            - "com.centurylinklabs.watchtower.scope=myscope"
```

## Unscoped Containers and Instances

### Unscoped Containers

A container has no scope when its `com.centurylinklabs.watchtower.scope` label is:

- Missing
- Empty
- Set to `none`

### Unscoped Instances

An instance has no scope when either of these is true:

- Its [`scope`](../../configuration/container-selection/index.md#scope_filter) configuration option is set to `none`.
- Neither its [`scope`](../../configuration/container-selection/index.md#scope_filter) configuration option nor its own label sets a scope.

An unscoped instance:

- Manages only unscoped containers
- Removes only other unscoped instances

!!! Tip
    Unscoped and scoped instances can run on the same host, since each leaves the other's containers alone.

### Example: Unscoped Instance

In this Docker Compose file, `watchtower` manages all three apps and itself, and it leaves the `myscope` containers from the first example alone:

```yaml
services:
    app-a:
        image: myapps/app-a

    app-b:
        image: myapps/app-b
        labels:
            - "com.centurylinklabs.watchtower.scope=none"

    app-c:
        image: myapps/app-c
        labels:
            - "com.centurylinklabs.watchtower.scope="

    watchtower:
        image: nickfedor/watchtower
        volumes:
            - "/var/run/docker.sock:/var/run/docker.sock"
        environment:
            - WATCHTOWER_SCOPE=none
```

## Scope From the Container Label

Without the [`scope`](../../configuration/container-selection/index.md#scope_filter) configuration option, Watchtower reads its scope from the `com.centurylinklabs.watchtower.scope` label of its own container. An instance can therefore be scoped by that label alone.

### Example: Scope From a Label

In this Docker Compose file, `watchtower` has no [`scope`](../../configuration/container-selection/index.md#scope_filter) configuration option and takes the scope `myscope` from its label:

```yaml
services:
    watchtower:
        image: nickfedor/watchtower
        volumes:
            - "/var/run/docker.sock:/var/run/docker.sock"
        labels:
            - "com.centurylinklabs.watchtower.scope=myscope"
```

### When the Label Cannot Be Read

If Watchtower cannot inspect its own container at startup, then it exits with an error rather than run without a scope.

- Before it exits, it disables its container's restart policy. Start it again once the cause is fixed.
- If the restart policy cannot be disabled, then the error notification says so. The restart policy may then start Watchtower again.

!!! Tip
    Setting the [`scope`](../../configuration/container-selection/index.md#scope_filter) configuration option avoids the dependency on the label. The option takes precedence over the label, and `none` counts as a set value.
