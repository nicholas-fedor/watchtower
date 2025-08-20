# Configuration

## Overview

Watchtower uses [Shoutrrr](https://github.com/nicholas-fedor/shoutrrr){target="_blank" rel="noopener noreferrer"} to provide notification functionality.
Notifications are sent via hooks in the [logrus](http://github.com/sirupsen/logrus){target="_blank" rel="noopener noreferrer"} logging system.

Watchtower will post a notification every time it is started.
This behavior can be changed with an [argument](../../configuration/arguments/index.md#disable_startup_message).

!!! Note "Using multiple notifications with environment variables"
    There is currently a bug in Viper ([Issue](https://github.com/spf13/viper/issues/380){target="_blank" rel="noopener noreferrer"}), which prevents comma-separated slices to
    be used when using the environment variable.

    A workaround is available where we instead put quotes around the environment variable value and replace the commas with
    spaces:

    ```
    WATCHTOWER_NOTIFICATIONS="slack msteams"
    ```

    If you're a `docker-compose` user, make sure to specify environment variables' values in your `.yml` file without double
    quotes (`"`). This prevents unexpected errors when watchtower starts.

## General Notification Settings

### Notifications Level

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notifications-level` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATIONS_LEVEL` |
| **Description** | Controls the log level for notifications. Defaults to `info`. In legacy mode (`--notification-report=false`), only `info`-level logs trigger notifications, ensuring a focused step-by-step update summary. Possible values: `panic`, `fatal`, `error`, `warn`, `info`, `debug`, `trace`. |

### Notifications Hostname

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notifications-hostname` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATIONS_HOSTNAME` |
| **Description** | Custom hostname specified in subject/title. Useful to override the operating system hostname. |

### Notifications Delay

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notifications-delay` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATIONS_DELAY` |
| **Description** | Delay before sending notifications expressed in seconds. |

### Notification Title Tag

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-title-tag` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_TITLE_TAG` |
| **Description** | Prefix to include in the title. Useful when running multiple watchtowers. |

### Notification Skip Title

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-skip-title` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_SKIP_TITLE` |
| **Description** | Do not pass the title param to notifications. This will not pass a dynamic title override to notification services. If no title is configured for the service, it will remove the title altogether. |

### Notification Log Stdout

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-log-stdout` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_LOG_STDOUT` |
| **Description** | Enable output from `logger://` shoutrrr service to stdout. |

## Legacy Notifications

For backwards compatibility, the notifications can also be configured using legacy notification options. These will automatically be converted to shoutrrr URLs when used.
The types of notifications to send are set by passing a comma-separated list of values to the `--notifications` option
(or corresponding environment variable `WATCHTOWER_NOTIFICATIONS`), which has the following valid values:

- `email` to send notifications via e-mail
- `slack` to send notifications through a Slack webhook
- `msteams` to send notifications via MSTeams webhook
- `gotify` to send notifications via Gotify

### `notify-upgrade`

If watchtower is started with `notify-upgrade` as it's first argument, it will generate a .env file with your current legacy notification options converted to shoutrrr URLs.
<!-- markdownlint-disable -->
=== "docker run"

    ```bash
    $ docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -e WATCHTOWER_NOTIFICATIONS=slack \
    -e WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL="https://hooks.slack.com/services/xxx/yyyyyyyyyyyyyyy" \
    nickfedor/watchtower \
    notify-upgrade
    ```

=== "docker-compose.yml"

    ```yaml
    version: "3"
    services:
      watchtower:
        image: nickfedor/watchtower
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
        env:
          WATCHTOWER_NOTIFICATIONS: slack
          WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL: https://hooks.slack.com/services/xxx/yyyyyyyyyyyyyyy
        command: notify-upgrade
    ```
<!-- markdownlint-restore -->
You can then copy this file from the container (a message with the full command to do so will be logged) and use it with your current setup:
<!-- markdownlint-disable -->
=== "docker run"

    ```bash
    $ docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    --env-file watchtower-notifications.env \
    nickfedor/watchtower
    ```

=== "docker-compose.yml"

    ```yaml
    version: "3"
    services:
      watchtower:
        image: nickfedor/watchtower
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
        env_file:
          - watchtower-notifications.env
    ```
<!-- markdownlint-restore -->

## Email Notifications

To receive notifications by email, the following command-line options, or their corresponding environment variables, can be set:

### Email From

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-from` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_FROM` |
| **Description** | The e-mail address from which notifications will be sent. |

### Email To

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-to` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_TO` |
| **Description** | The e-mail address to which notifications will be sent. |

### Email Server

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-server` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_SERVER` |
| **Description** | The SMTP server to send e-mails through. |

### Email Server TLS Skip Verify

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-server-tls-skip-verify` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_TLS_SKIP_VERIFY` |
| **Description** | Do not verify the TLS certificate of the mail server. This should be used only for testing. |

### Email Server Port

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-server-port` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT` |
| **Description** | The port used to connect to the SMTP server to send e-mails through. Defaults to `25`. |

### Email Server User

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-server-user` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER` |
| **Description** | The username to authenticate with the SMTP server with. |

### Email Server Password

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-server-password` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD` |
| **Description** | The password to authenticate with the SMTP server with. Can also reference a file, in which case the contents of the file are used. |

### Email Delay

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-delay` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_DELAY` |
| **Description** | Delay before sending notifications expressed in seconds. |

### Email Subject Tag

| Property | Value |
|----------|-------|
| **CLI Flag** | `--notification-email-subjecttag` |
| **Environment Variable** | `WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG` |
| **Description** | Prefix to include in the subject tag. Useful when running multiple watchtowers. **NOTE:** This will affect all notification types. |

Example:

```bash
docker run -d \
  --name watchtower \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_NOTIFICATIONS=email \
  -e WATCHTOWER_NOTIFICATION_EMAIL_FROM=fromaddress@gmail.com \
  -e WATCHTOWER_NOTIFICATION_EMAIL_TO=toaddress@gmail.com \
  -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER=smtp.gmail.com \
  -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT=587 \
  -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER=fromaddress@gmail.com \
  -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD=app_password \
  -e WATCHTOWER_NOTIFICATION_EMAIL_DELAY=2 \
  nickfedor/watchtower
```

The previous example assumes, that you already have an SMTP server up and running you can connect to. If you don't or you want to bring up watchtower with your own simple SMTP relay the following `docker-compose.yml` might be a good start for you.

The following example assumes, that your domain is called `your-domain.com` and that you are going to use a certificate valid for `smtp.your-domain.com`. This hostname has to be used as `WATCHTOWER_NOTIFICATION_EMAIL_SERVER` otherwise the TLS connection is going to fail with `Failed to send notification email` or `connect: connection refused`. We also have to add a network for this setup in order to add an alias to it. If you also want to enable DKIM or other features on the SMTP server, you will find more information at [freinet/postfix-relay](https://hub.docker.com/r/freinet/postfix-relay){target="_blank" rel="noopener noreferrer"}.

Example including an SMTP relay:

```yaml
version: '3.8'
services:
  watchtower:
    image: nickfedor/watchtower:latest
    container_name: watchtower
    environment:
      WATCHTOWER_MONITOR_ONLY: 'true'
      WATCHTOWER_NOTIFICATIONS: email
      WATCHTOWER_NOTIFICATION_EMAIL_FROM: from-address@your-domain.com
      WATCHTOWER_NOTIFICATION_EMAIL_TO: to-address@your-domain.com
      # you have to use a network alias here, if you use your own certificate
      WATCHTOWER_NOTIFICATION_EMAIL_SERVER: smtp.your-domain.com
      WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT: 25
      WATCHTOWER_NOTIFICATION_EMAIL_DELAY: 2
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    networks:
      - watchtower
    depends_on:
      - postfix

  # SMTP needed to send out status emails
  postfix:
    image: freinet/postfix-relay:latest
    expose:
      - 25
    environment:
      MAILNAME: somename.your-domain.com
      TLS_KEY: '/etc/ssl/domains/your-domain.com/your-domain.com.key'
      TLS_CRT: '/etc/ssl/domains/your-domain.com/your-domain.com.crt'
      TLS_CA: '/etc/ssl/domains/your-domain.com/intermediate.crt'
    volumes:
      - /etc/ssl/domains/your-domain.com/:/etc/ssl/domains/your-domain.com/:ro
    networks:
      watchtower:
        # this alias is really important to make your certificate work
        aliases:
          - smtp.your-domain.com
networks:
  watchtower:
    external: false
```

## Slack Notifications

To receive notifications in Slack, add `slack` to the `--notifications` option or the `WATCHTOWER_NOTIFICATIONS` environment variable.

Additionally, you should set the Slack webhook URL using the `--notification-slack-hook-url` option or the `WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL` environment variable. This option can also reference a file, in which case the contents of the file are used.

By default, watchtower will send messages under the name `watchtower`, you can customize this string through the `--notification-slack-identifier` option or the `WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER` environment variable.

Other, optional, variables include:

- `--notification-slack-channel` (env. `WATCHTOWER_NOTIFICATION_SLACK_CHANNEL`): A string which overrides the webhook's default channel. Example: #my-custom-channel.

Example:

```bash
docker run -d \
  --name watchtower \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_NOTIFICATIONS=slack \
  -e WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL="https://hooks.slack.com/services/xxx/yyyyyyyyyyyyyyy" \
  -e WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER=watchtower-server-1 \
  -e WATCHTOWER_NOTIFICATION_SLACK_CHANNEL=#my-custom-channel \
  nickfedor/watchtower
```

## Microsoft Teams Notifications

To receive notifications in MSTeams channel, add `msteams` to the `--notifications` option or the `WATCHTOWER_NOTIFICATIONS` environment variable.

Additionally, you should set the MSTeams webhook URL using the `--notification-msteams-hook` option or the `WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL` environment variable. This option can also reference a file, in which case the contents of the file are used.

MSTeams notifier could send keys/values filled by `log.WithField` or `log.WithFields` as MSTeams message facts. To enable this feature add `--notification-msteams-data` flag or set `WATCHTOWER_NOTIFICATION_MSTEAMS_USE_LOG_DATA=true` environment variable.

Example:

```bash
docker run -d \
  --name watchtower \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_NOTIFICATIONS=msteams \
  -e WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL="https://outlook.office.com/webhook/xxxxxxxx@xxxxxxx/IncomingWebhook/yyyyyyyy/zzzzzzzzzz" \
  -e WATCHTOWER_NOTIFICATION_MSTEAMS_USE_LOG_DATA=true \
  nickfedor/watchtower
```

## Gotify Notifications

To push a notification to your Gotify instance, register a Gotify app and specify the Gotify URL and app token:

```bash
docker run -d \
  --name watchtower \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_NOTIFICATIONS=gotify \
  -e WATCHTOWER_NOTIFICATION_GOTIFY_URL="https://my.gotify.tld/" \
  -e WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN="SuperSecretToken" \
  nickfedor/watchtower
```

`-e WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN` or `--notification-gotify-token` can also reference a file, in which case the contents of the file are used.

If you want to disable TLS verification for the Gotify instance, you can use either `-e WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY=true` or `--notification-gotify-tls-skip-verify`.
