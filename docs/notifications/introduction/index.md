# Introduction

## Overview

Watchtower uses [Shoutrrr](https://github.com/nicholas-fedor/shoutrrr){target="_blank" rel="noopener noreferrer"} to send notifications.

### Enabling Notifications

To send notifications, use the [`NOTIFICATION URL`](../../configuration/notifications/index.md#notification_url) configuration option to specify the Shoutrrr service URL.

The Shoutrrr URL follows the format:

```text
<service>://<required-credentials>[:<optional-credentials>]@<required-service>/<required-path>?<key>=<value>&...
```

The format is the same for all services, but the parameters, path, and credentials vary between them.

The [`NOTIFICATION URL`](../../configuration/notifications/index.md#notification_url) configuration option can also reference a file, in which case the contents of the file are used.

### Using Multiple Notification Services

Watchtower supports sending notifications to multiple services simultaneously.
The preferred method is to use multiple Shoutrrr URL's.

For most Watchtower deployments via Docker Compose, this is best achieved via using either a comma-separated list or YAML array for the [`WATCHTOWER_NOTIFICATION_URL`](../../configuration/notifications/index.md#notification_url) environment variable.
When running Watchtower via the Docker CLI, the [`--notification-url`](../../configuration/notifications/index.md#notification_url) CLI flag can be used multiple times, or use a comma-separated list.

!!! Note "Environment Variable Format"
    - `WATCHTOWER_NOTIFICATION_URL` supports comma-separated and space-separated values.
    - Commas within URLs (e.g., in query parameters) are preserved.
    - For Docker Compose, the YAML array syntax is the recommended approach.

=== "Docker CLI"

    === "Environment Variable"

        ```bash
        docker run -d \
        --name watchtower \
        -v /var/run/docker.sock:/var/run/docker.sock \
        -e WATCHTOWER_NOTIFICATION_URL="discord://token@webhookid,telegram://token@telegram?chats=@channel" \
        nickfedor/watchtower
        ```

    === "Flags"

        ```bash
        docker run -d \
        --name watchtower \
        -v /var/run/docker.sock:/var/run/docker.sock \
        nickfedor/watchtower \
        --notification-url "discord://token@webhookid" \
        --notification-url "telegram://token@telegram?chats=@channel"
        ```

=== "Docker Compose"

    === "YAML Array"

        ```yaml
        services:
        watchtower:
            image: nickfedor/watchtower:latest
            environment:
            WATCHTOWER_NOTIFICATION_URL:
                - "discord://token@webhookid"
                - "telegram://token@telegram?chats=@channel"
            volumes:
            - /var/run/docker.sock:/var/run/docker.sock
        ```

    === "Single-line"

        ```yaml
        services:
          watchtower:
            image: nickfedor/watchtower:latest
            environment:
              WATCHTOWER_NOTIFICATION_URL: "discord://token@webhookid,telegram://token@telegram?chats=@channel"
            volumes:
              - /var/run/docker.sock:/var/run/docker.sock
        ```

    === "Multi-line"

        ```yaml
        services:
        watchtower:
            image: nickfedor/watchtower:latest
            environment:
            WATCHTOWER_NOTIFICATION_URL: >
                discord://token@webhookid,
                telegram://token@telegram?chats=@channel
            volumes:
            - /var/run/docker.sock:/var/run/docker.sock
        ```

    !!! Warning "Do NOT define the variable multiple times"
        Defining `WATCHTOWER_NOTIFICATION_URL` multiple times in your environment
        will cause the last value to overwrite previous ones:

        ```yaml
        # WRONG - Only the second URL will be used:
        environment:
        - WATCHTOWER_NOTIFICATION_URL=discord://xxx
        - WATCHTOWER_NOTIFICATION_URL=telegram://xxx
        ```

!!! Note "CLI Flags vs Environment Variables"
    The CLI flag can be called multiple times as CLI arguments; however, defining the environment variable multiple times will NOT work and only the last value will be used.

    This is because CLI flags use a StringArray type that supports multiple invocations,  while environment variables are simple key-value pairs that get overwritten when defined multiple times.

    For environment variables, use comma-separated values or YAML arrays instead.

#### Verifying Multiple Notifications

When Watchtower starts, check the logs for the notification summary:

```text
time="2026-01-28T16:07:24+01:00" level=info msg="Using notifications: discord, telegram"
```

If you only see one service listed (e.g., `Using notifications: telegram`), your multiple URL configuration was not parsed correctly.

### Startup Notifications

Watchtower will log and send a notification every time it is started.

This behavior can be disabled with the [`DISABLE STARTUP MESSAGE`](../../configuration/notifications/index.md#disable_startup_message) configuration option.

## Notification Templates

Watchtower allows you to customize the format and content of notification messages using Go templates. You can define templates either inline or load them from a file.

### Inline Templates

Use the [`notification-template`](../../configuration/notifications/index.md#notification_template) configuration option to specify a template directly as a string.

### File-Based Templates

For more complex templates or better maintainability, use the [`notification-template-file`](../../configuration/notifications/index.md#notification_template_file) configuration option to specify a path to a template file.

!!! Note
    When both inline and file-based templates are specified, the file-based template takes precedence.

For detailed information about template syntax, available data structures, and examples, see the [Notification Templates](../templates/index.md) documentation.
