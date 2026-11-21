# Linked Containers

## Overview

Watchtower, by default, ensures that interdependent containers are updated in the correct order to maintain application stability.
It automatically detects container dependencies through various mechanisms and uses topological sorting to determine the optimal update sequence.

When containers depend-upon each other (such as a web application depending on a database), updating them in the wrong order can cause service disruptions.

Watchtower addresses this by:

- **Detecting dependencies** through Docker links, labels, and network configurations
- **Performing topological sorting** to determine the correct update order
- **Stopping containers in reverse dependency order** (dependents first - i.e., web app before database)
- **Restarting containers in dependency order** (dependencies first - i.e., database before web app)

This ensures that dependent services are stopped before their dependencies are updated, and dependencies are available when dependents restart.

## How It Works

### Dependency Detection

Watchtower detects container dependencies from labels and from each container's Docker configuration.

Labels are checked in the following priority order, and only the first one present is used:

1. **Watchtower depends-on label** (`com.centurylinklabs.watchtower.depends-on`)
2. **Docker Compose depends_on label** (`com.docker.compose.depends_on`)

An empty Watchtower depends-on label counts as absent, so the Docker Compose depends_on label is used instead when Compose detection is enabled.

The Docker configuration always counts as well, whichever label is present:

- **Docker links and network mode** (legacy Docker linking, `network_mode: service:SERVICE` for a Compose service, and `network_mode: container:CONTAINER` for a container name or ID)
- **Volumes from another container** (`volumes_from` in Compose, `--volumes-from` in the Docker CLI)

A container named by both a label and the Docker configuration is a single dependency.

#### Watchtower Depends-On Label

The `com.centurylinklabs.watchtower.depends-on` label allows explicit declaration of dependencies:

```dockerfile title="Explicit dependency declaration"
LABEL com.centurylinklabs.watchtower.depends-on="database,redis"
```

This label accepts a comma-separated list of container names that must be available before this container starts.
This supports referencing containers from other Docker Compose projects or stacks, enabling cross-project dependency management.

An entry may also name a Docker Compose service, such as `db` or `my-database`, or a project-qualified service, such as `myproject-db`.
A service name resolves to every replica of that service.

A service name used in more than one Compose project, such as `db`, resolves to the service in the labeled container's own project when that project has one.
Otherwise it resolves to the service in every project that has it, so the container restarts when any of them is updated.
When the service in its own project is unmonitored, the entry is left alone instead (see [Dependencies on Unmonitored Containers](#dependencies_on_unmonitored_containers)).
Use the container name or a project-qualified service name, such as `myproject-db`, to depend on only one of them.

!!! Warning
    An entry that names no container is ignored and logged as a warning.

#### Docker Compose Depends-On Label

Watchtower automatically recognizes Docker Compose's `depends_on` relationships:

```yaml title="Docker Compose dependency"
services:
  web:
    image: nginx
    depends_on:
      - database
  database:
    image: postgres
```

The `com.docker.compose.depends_on` label is automatically set by Docker Compose and parsed by Watchtower to extract service names and support implicitly restarting linked services.

<!-- markdownlint-disable MD046 -->
!!! Important "Docker Compose depends_on Restart Behavior"
    Docker Compose's [`depends_on`](https://docs.docker.com/reference/compose-file/services/#depends_on){target="_blank" rel="noopener noreferrer"} has a `restart` attribute in the long-form syntax:

    ```yaml
    services:
      web:
        depends_on:
          database:
            condition: service_healthy
            restart: true
    ```

    Watchtower does not support using this to control implicit restarts, because this is an explicit, opt-in feature that defaults to `false` when omitted, such as when using the short-form syntax.
<!-- markdownlint-enable MD046 -->

#### Docker Links and Network Mode

For legacy Docker links, and for containers that use another container's network (`network_mode: service:SERVICE` or `network_mode: container:CONTAINER` in Compose, `--network container:CONTAINER` in the Docker CLI), Watchtower treats these as implicit dependencies:

```dockerfile title="Legacy Docker linking"
# Container with explicit link
docker run --link database:db nginx

# Container using another container's network
docker run --network container:CONTAINER nginx
```

Replace *`CONTAINER`* with the name or ID of the container whose network to use.

These count as dependencies even when a depends-on or Compose `depends_on` label names other containers, because the container cannot run without the container whose network it uses.
The same applies to `volumes_from` (`--volumes-from`): the container is always ordered after the container whose volumes it uses.

### Topological Sorting

Watchtower uses topological sorting to determine the correct update order. This algorithm:

- Builds a dependency graph from all detected relationships
- Detects cycles and sets aside every container in one
- Produces a linear ordering where dependencies precede dependents

!!! Warning
    Containers in a circular dependency are skipped and reported as skipped, while the rest of the update continues.
    Ensure your dependency graph is acyclic so those containers are updated.

### Update Order

When updates are needed, Watchtower follows this sequence:

1. **Identify all containers requiring updates**
2. **Expand the set** to include all containers in the dependency chain
3. **Sort containers** using topological order (dependencies first)
4. **Stop containers** in reverse topological order (dependents first)
5. **Update and restart containers** in topological order (dependencies first)

This ensures that:

- Dependent services are stopped before their dependencies change
- Dependencies are fully restarted before dependents attempt to connect

### Restarting Dependent Containers

When a container is updated, Watchtower also restarts the containers that depend on it, directly or through other restarted containers, without updating their images.
Dependents are found with the same dependency graph that orders the update, so the containers restarted and the update order always agree.

- A Watchtower container restarts when a container it depends on restarts, such as a VPN container it shares a network with.
- A container that names a Watchtower container exactly, by container name or ID, restarts when that Watchtower container is updated. The name without its replica number (such as `infra-watchtower` for `infra-watchtower-1`) also works when exactly one Watchtower container has it, so it names nothing when the service runs several replicas.
- Containers in a circular dependency are neither updated nor restarted.

### Containers Sharing a Recreated Container's Network

Docker records the container that `network_mode: container:CONTAINER`, `network_mode: service:SERVICE` or `--network container:CONTAINER` names by its ID.
When Watchtower recreates that container, any container that joins its network still refers to the removed one, so it loses its network if it is running and can no longer start if it is stopped.
Watchtower therefore recreates such a container after the container whose network it joins, so that it joins the new one.

#### Which Containers Are Recreated

Watchtower recreates every container that joins the network of a container it recreates, whether that container is running, stopped, paused or restarting.
A container that joins the network of one of these is recreated in turn.
This includes containers Watchtower would otherwise leave alone:

- A monitor-only container.
- A container excluded from monitoring, such as one without the enable label or in another instance's scope.
- A container skipped because of a circular dependency or a dependency on itself.

Any other monitored container that joins the network is restarted as a dependent of the container it joins instead (see [Restarting Dependent Containers](#restarting_dependent_containers)), so its lifecycle hooks run and the remove-volumes configuration option applies to it.

Watchtower never recreates these containers this way:

- Its own containers and the containers that join a Watchtower container's network. Its self-update replaces its container only after the running instance hands over, which is too late to recreate them against the new container.
- A run of a Docker Engine job (see [Compose Jobs](../compose-jobs/index.md)).
- A container built from Git in the same session while the no-restart configuration option is set, which keeps running as it is with the removed container's network.

When Compose recreates a joining container during a Git-monitored apply in the same session as Watchtower recreates the container it joins, the new container still refers to the removed one.

#### How a Recreated Container Is Handled

- It keeps its image. A monitor-only container is still checked for updates and reported as usual.
- It keeps its anonymous volumes even when the remove-volumes configuration option is set. The exception is a running container started with `--rm`, whose anonymous volumes Docker removes along with the stopped container.
- Its replacement records the kept volumes in the `com.centurylinklabs.watchtower.kept-volumes` label. A later recreation that does not keep them, such as an update, gives it new anonymous volumes as for any other container.
- Volumes it takes from another container through volumes-from still come from that container.
- A running or paused container starts again unless the no-restart configuration option is set, while a stopped container stays stopped even with the revive-stopped configuration option. A paused container comes back running.
- Its pre-update and post-update commands do not run. Its pre-check and post-check commands run when Watchtower monitors it.

#### Recreation Order

A recreated container follows the container whose network it joins, along with any container Watchtower recreates that its links or volumes-from name, since Docker resolves those when it creates the container.
A monitored container also follows the containers it depends on.

#### Replacing the Shared Container

Watchtower replaces the shared container before it removes the containers that join it:

1. It stops the other containers it recreates with the shared container, in reverse order. These are the containers placed between the shared container and a joining container that waits for them, such as one its links name. Each one's pre-update command runs right before it stops.
2. It runs the shared container's pre-update command.
3. It renames the shared container to `NAME-replaced-ID`, where `NAME` is its name and `ID` its short ID.
4. It creates the replacement under the original name.
5. It removes the containers that join the shared container, then the shared container itself. These removals run to their end even when the update is canceled, since Docker finishes a stop it has begun.
6. It recreates the removed containers against the replacement.

Without rolling restarts, the shared container is not replaced early when its own network mode, links or volumes-from name another container Watchtower recreates.
In that case the joining containers are removed first and the replacement is created only after that other container's.

With rolling restarts, a monitored container that joins the shared container is stopped only in its own turn, after the shared container is replaced.
It has no network until then. On Podman the shared container cannot be removed while it runs, so its replacement is undone.

#### Failures

If the replacement cannot be created before the joining containers are removed, the shared container takes its name back and those containers are left untouched.
When the replacement is created only after they were removed, a failed create leaves the shared container and the joining containers removed. Watchtower reports each of them.

A container stopped in step 1 that joins the network of another container in the group and cannot be stopped, such as one whose pre-update command skips it, keeps the shared container in place.
Watchtower then renames nothing. It reports the shared container as failed and skips the containers of its group it did not reach.

If a joining container cannot be stopped or the shared container cannot be removed, Watchtower removes the replacement and gives the shared container its name back.
It starts the shared container again if it was running, then recreates the joining containers it already removed so they join it again.
The joining container that could not be stopped keeps its place in the restored network and starts again if it was running, while those not reached yet are skipped.
On Podman, a paused joining container cannot be stopped, so the shared container stays as it is.

When the removal of the shared container fails but the daemon removed it anyway, its replacement counts as done.
If Watchtower cannot tell whether it was removed, it keeps the replacement and reports the shared container as failed, since removing the replacement could leave neither container.

Without rolling restarts, a container that joins another's network and is not stopped keeps the container it joins running too, so Watchtower reports that container as not updated.

Watchtower reports a container it cannot recreate as "Could not reconnect container to the network of a recreated container" and lists it as failed, also when it does not monitor that container.
A joining container that cannot be stopped keeps its network, so the shared container's failure reports it.

### Dependencies on Unmonitored Containers

A container that Watchtower does not monitor is left alone, including when a monitored container depends on it, unless it joins the network of a container Watchtower recreates (see [Containers Sharing a Recreated Container's Network](#containers_sharing_a_recreated_containers_network)).
Containers are excluded from monitoring by the [container selection](../../configuration/container-selection/index.md) options, such as the enable label, the disable lists, image filters, and scopes.

- A dependency on an unmonitored container is ignored when ordering updates and restarting dependents.
- A dependency that names an unmonitored container exactly never resolves to a different, monitored container with a similar name.
- A service name whose service in the container's own Compose project is unmonitored never resolves to the same service in another project.

## Configuration

### Automatic Detection

In most cases, no additional configuration is required. Watchtower automatically detects dependencies from:

- Docker Compose `depends_on` declarations
- Existing Docker links
- `network_mode: service:SERVICE` and `network_mode: container:CONTAINER` configurations
- `volumes_from` configurations

!!! Note "Docker Compose Considerations"
    When using Docker Compose, Watchtower leverages `depends_on` declarations for dependency detection. Dependencies are resolved using service names, not container names. Ensure your `depends_on` references service names correctly.

!!! Warning "Rolling restart is currently not supported when used in combination with linked-containers."
    This limitation exists because linked-containers require coordinated updates across dependency chains, which conflicts with the incremental nature of rolling restarts.

### Disable Docker Compose Depends-On

If you want to disable automatic dependency detection from the Docker Compose `depends_on` configuration while preserving other dependency sources, use the following configuration:

<!-- markdownlint-disable MD046 -->
=== "Docker Compose"
    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower
        environment:
          - WATCHTOWER_USE_COMPOSE_DEPENDS_ON=false
    ```
=== "Docker CLI"
    ```bash
    docker run -d \
        --name watchtower \
        -v /var/run/docker.sock:/var/run/docker.sock \
        nickfedor/watchtower \
        --use-compose-depends-on=false
    ```
<!-- markdownlint-enable MD046 -->

This disables parsing of the `com.docker.compose.depends_on` label while still honoring:

- Watchtower's explicit `com.centurylinklabs.watchtower.depends-on` label
- Legacy Docker links
- Network mode dependencies (`network_mode: service:SERVICE` and `network_mode: container:CONTAINER`)
- Volumes from another container (`volumes_from`)

### Explicit Dependencies

For cases where automatic detection is insufficient, use the Watchtower depends-on label:

<!-- markdownlint-disable MD046 -->
=== "Dockerfile"
    ```dockerfile
    FROM nginx:latest

    # Declare dependencies on database and cache services
    LABEL com.centurylinklabs.watchtower.depends-on="postgres,redis"
    ```
=== "Docker Compose"
    ```yaml
    services:
      web:
        image: nginx
        labels:
          - com.centurylinklabs.watchtower.depends-on=postgres,redis
      postgres:
        image: postgres
      redis:
        image: redis
    ```
<!-- markdownlint-enable MD046 -->

### Advanced Scenarios

#### Multiple Dependencies

```dockerfile title="Multiple dependencies"
LABEL com.centurylinklabs.watchtower.depends-on="database,cache,queue"
```

#### Complex Dependency Chains

```yaml title="Complex dependency chain"
services:
  web:
    image: nginx
    depends_on:
      - api
  api:
    image: myapi
    depends_on:
      - database
      - cache
  database:
    image: postgres
  cache:
    image: redis
```

In this scenario, Watchtower will update containers in the order: `cache`, `database`, `api`, `web`.

## Examples

### MySQL-WordPress Scenario

Consider a classic WordPress setup with MySQL database:

<!-- markdownlint-disable MD046 -->
=== "Docker Compose"
    ```yaml
    services:
      wordpress:
        image: wordpress:latest
        depends_on:
          - mysql
        environment:
          WORDPRESS_DB_HOST: mysql
      mysql:
        image: mysql:8.0
        environment:
          MYSQL_ROOT_PASSWORD: 'DB_ROOT_PASSWORD'
    ```

    Replace *`DB_ROOT_PASSWORD`* with a root password for the database, inside the single quotes so that YAML reads every character as part of it. Inside the quotes, write each `'` as `''`. Write each `$` as `$$`, because Compose reads a `$` as the start of a variable even inside quotes.

=== "Update Process"
    When Watchtower detects a MySQL update:

    1. **Dependency Detection**: Identifies that `wordpress` depends on `mysql`
    2. **Stop Order**: Stops `wordpress` first, then `mysql`
    3. **Update Order**: Updates and restarts `mysql` first, then `wordpress`

    This prevents WordPress from losing database connectivity during the update.
<!-- markdownlint-enable MD046 -->

### Microservices Architecture

For complex applications with multiple services:

<!-- markdownlint-disable MD046 -->
=== "Docker Compose"

    ```yaml
    services:
      api-gateway:
        image: nginx
        depends_on:
          - auth-service
          - user-service
      auth-service:
        image: auth-service
        depends_on:
          - redis
      user-service:
        image: user-service
        depends_on:
          - postgres
      redis:
        image: redis
      postgres:
        image: postgres
    ```

=== "Dependency Graph"

    ```
    postgres → user-service
    redis → auth-service
    auth-service → api-gateway
    user-service → api-gateway
    ```

    Update order: `postgres`, `redis`, `user-service`, `auth-service`, `api-gateway`
<!-- markdownlint-enable MD046 -->

### Legacy Docker Links

For applications using traditional Docker linking:

<!-- markdownlint-disable MD046 -->
=== "Docker Run Commands"

    ```bash
    # Start database
    docker run -d --name mysql mysql:8.0

    # Start web app with link
    docker run -d --name webapp --link mysql:db nginx
    ```

=== "Watchtower Behavior"

    Watchtower automatically detects the link and ensures `webapp` is stopped before `mysql` updates, and `mysql` restarts before `webapp`.

### Network Mode Dependencies

Containers using `network_mode: service:SERVICE`:

=== "Docker Compose"

    ```yaml
    services:
      sidecar:
        image: sidecar
        network_mode: service:main-app
      main-app:
        image: main-app
    ```

=== "Watchtower Behavior"

    The `sidecar` container is treated as dependent on `main-app`, ensuring proper update sequencing.
<!-- markdownlint-enable MD046 -->

## Troubleshooting

### Common Issues

Watchtower reports each of the following dependency problems once per container.
The first scan that finds a problem logs and notifies it as a warning.
Later scans log it at debug level.
Every scan still skips a container that depends on itself or sits in a circular dependency, and lists it as skipped in the session report.
A container depends on itself when one of its dependencies names it by its container name or by its own Compose service.
A skipped container is not restarted when another of its dependencies is updated.
A depends-on entry that names no container is only ignored, and its container is still checked and updated.
A recreated container or a restart of Watchtower reports the problem again.

#### Containers Skipped Due to Circular Dependencies

!!! Warning
    If you see "Skipping container update (circular dependency)" warnings, check your dependency declarations for cycles.

**Solution**: Review and remove circular dependencies. For example, if A depends on B and B depends on A, remove one of the dependencies or restructure your services.

#### Ignored Depends-On Entries

!!! Warning
    If you see "Ignoring depends-on entry that names no container" warnings, the named `com.centurylinklabs.watchtower.depends-on` entry matched no container.

**Solution**: Use the exact container name, a Compose service name, or a project-qualified service name such as `myproject-db`.

#### Containers Not Updating in Expected Order

**Check**:

- Verify dependency labels are correctly formatted
- Ensure container names match exactly
- Check Docker Compose service names vs container names

#### Missing Dependencies

**Symptoms**: Containers update out of order or fail to connect after updates.

**Debug**: Enable debug logging to see detected dependencies:

```bash
watchtower --debug
```

Look for log messages like:

- "Found container dependencies", which lists each container's dependencies once per scan

### Debugging Commands

Enable verbose logging to inspect dependency detection:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  nickfedor/watchtower \
  --debug \
  --run-once
```

Check container labels:

```bash
docker inspect container_name | jq '.[0].Config.Labels'
```
