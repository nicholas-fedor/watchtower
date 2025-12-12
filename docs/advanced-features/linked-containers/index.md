# Linked Containers

Watchtower's linked containers feature ensures that interdependent containers are updated in the correct order to maintain application stability.
This feature automatically detects container dependencies through various mechanisms and uses topological sorting to determine the optimal update sequence.

## Overview

When containers have dependencies on each other (such as a web application depending on a database), updating them in the wrong order can cause service disruptions.

Watchtower addresses this by:

- **Detecting dependencies** through Docker links, labels, and network configurations
- **Performing topological sorting** to determine the correct update order
- **Stopping containers in reverse dependency order** (dependents first - i.e., web app before database)
- **Restarting containers in dependency order** (dependencies first - i.e., database before web app)

This ensures that dependent services are stopped before their dependencies are updated, and dependencies are available when dependents restart.

## How It Works

### Dependency Detection

Watchtower detects container dependencies through multiple mechanisms, checked in the following priority order:

1. **Watchtower depends-on label** (`com.centurylinklabs.watchtower.depends-on`)
2. **Docker Compose depends_on label** (`com.docker.compose.depends_on`)
3. **Docker links and network mode** (legacy Docker linking and `network_mode: service:container`)

#### Watchtower Depends-On Label

The `com.centurylinklabs.watchtower.depends-on` label allows explicit declaration of dependencies:

```dockerfile title="Explicit dependency declaration"
LABEL com.centurylinklabs.watchtower.depends-on="database,redis"
```

This label accepts a comma-separated list of container names that must be available before this container starts.

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

The `com.docker.compose.depends_on` label is automatically set by Docker Compose and parsed by Watchtower to extract service names.

#### Docker Links and Network Mode

For legacy Docker setups using links or `network_mode: service:container`, Watchtower treats these as implicit dependencies:

```dockerfile title="Legacy Docker linking"
# Container with explicit link
docker run --link database:db nginx

# Container using service network mode
docker run --network container:database nginx
```

### Topological Sorting

Watchtower uses topological sorting to determine the correct update order. This algorithm:

- Builds a dependency graph from all detected relationships
- Performs depth-first search to detect cycles (which would cause update failures)
- Produces a linear ordering where dependencies come before dependents

!!! Warning
    Circular dependencies between containers will cause the update process to fail with an error.
    Ensure your dependency graph is acyclic.

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

## Configuration

### Automatic Detection

In most cases, no additional configuration is required. Watchtower automatically detects dependencies from:

- Docker Compose `depends_on` declarations
- Existing Docker links
- `network_mode: service:container` configurations

### Explicit Dependencies

For cases where automatic detection is insufficient, use the Watchtower depends-on label:

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
          MYSQL_ROOT_PASSWORD: example
    ```

=== "Update Process"

    When Watchtower detects a MySQL update:

    1. **Dependency Detection**: Identifies that `wordpress` depends on `mysql`
    2. **Stop Order**: Stops `wordpress` first, then `mysql`
    3. **Update Order**: Updates and restarts `mysql` first, then `wordpress`

    This prevents WordPress from losing database connectivity during the update.

### Microservices Architecture

For complex applications with multiple services:

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

### Legacy Docker Links

For applications using traditional Docker linking:

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

Containers using `network_mode: service:container`:

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

## Troubleshooting

### Common Issues

#### Updates Failing Due to Circular Dependencies

!!! Error
    If you see "circular reference detected" errors, check your dependency declarations for cycles.

**Solution**: Review and remove circular dependencies. For example, if A depends on B and B depends on A, remove one of the dependencies or restructure your services.

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

- "Retrieved links from watchtower depends-on label"
- "Retrieved links from compose depends-on label"
- "Completed dependency sort"

#### Docker Compose Service Names

!!! Note
    When using Docker Compose, dependencies are resolved using service names, not container names. Ensure your `depends_on` references service names correctly.

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

### Best Practices

- **Use Docker Compose** for complex applications - it provides clear dependency declarations
- **Avoid circular dependencies** - they prevent successful updates
- **Test update scenarios** in staging environments first
- **Use explicit labels** when automatic detection doesn't work
- **Monitor logs** during updates to verify correct ordering
