# General Notification Settings

## Level

Controls the log level for notifications.

```text
            Argument: --notifications-level
Environment Variable: WATCHTOWER_NOTIFICATIONS_LEVEL
                Type: String
             Default: info
```

Possible values: `panic`, `fatal`, `error`, `warn`, `info`, `debug`, `trace`.

## Hostname

Custom hostname specified in subject/title.

```text
            Argument: --notifications-hostname
Environment Variable: WATCHTOWER_NOTIFICATIONS_HOSTNAME
                Type: String
             Default: None
```

!!! Note "This is useful for overriding the operating system hostname."

## Delay

Delay before sending notifications expressed in seconds. Watchtower exits at startup if the value is negative.

```text
            Argument: --notifications-delay
Environment Variable: WATCHTOWER_NOTIFICATIONS_DELAY
                Type: Integer
             Default: None
```

## Title Tag

Prefix to include in the title.

```text
            Argument: --notification-title-tag
Environment Variable: WATCHTOWER_NOTIFICATION_TITLE_TAG
                Type: String
             Default: None
```

!!! Note "This is useful when running multiple Watchtower instances."

## Skip Title

Disable passing the title parameter to notifications.

```text
            Argument: --notification-skip-title
Environment Variable: WATCHTOWER_NOTIFICATION_SKIP_TITLE
                Type: Boolean
             Default: false
```

!!! Note
    - This will not pass a dynamic title override to notification services.
    - If no title is configured for the service, it will remove the title altogether.

## Log Stdout

Enable output from `logger://` Shoutrrr service to stdout.

```text
            Argument: --notification-log-stdout
Environment Variable: WATCHTOWER_NOTIFICATION_LOG_STDOUT
                Type: Boolean
             Default: false
```

!!! Note
    Only the `logger://` output goes to stdout.
    Shoutrrr's own messages, such as a warning that a service sends without TLS, go to Watchtower's log at their level.

## Split by Container

Send a separate notification for each container instead of grouping them.

```text
            Argument: --notification-split-by-container
Environment Variable: WATCHTOWER_NOTIFICATION_SPLIT_BY_CONTAINER
                Type: Boolean
             Default: false
```

!!! Note
    - By default, notifications are grouped for all updated containers in a single session.
    - When split notifications is enabled, a separate notification is sent for each of these containers:
        - a container that was updated or restarted
        - a container whose update failed
        - a monitor-only container with a newer image
        - a container skipped during its update cooldown
    - Messages that none of these notifications carries are sent after them, one notification per container. A failed update check and a disk-space warning are examples. Messages that name no container are sent together.
