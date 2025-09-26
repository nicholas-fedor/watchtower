# Configuration

## Overview

Watchtower uses [Shoutrrr](https://github.com/nicholas-fedor/shoutrrr){target="_blank" rel="noopener noreferrer"} to provide notification functionality.
Notifications are sent via hooks in the [logrus](http://github.com/sirupsen/logrus){target="_blank" rel="noopener noreferrer"} logging system.

### Enabling Notifications

To send notifications, use the [`NOTIFICATION URL`](../../configuration/arguments/index.md#notification_url) configuration option to specify the Shoutrrr service URL.

The Shoutrrr URL follows the format:

```text
<service>://<required-credentials>[:<optional-credentials>]@<required-service>/<required-path>?<key>=<value>&...
```

The format is the same for all services, but the parameters, path, and credentials vary between them.

The [`NOTIFICATION URL`](../../configuration/arguments/index.md#notification_url) configuration option can also reference a file, in which case the contents of the file are used.

### Using Multiple Notification Services

The [`NOTIFICATION URL`](../../configuration/arguments/index.md#notification_url) configuration option can also be used multiple times or use a comma-separated list in the `WATCHTOWER_NOTIFICATION_URL` environment variable to utilize multiple notification services.

!!! Note "Using multiple notifications with environment variables"
    There is currently a bug in Viper ([Issue](https://github.com/spf13/viper/issues/380){target="_blank" rel="noopener noreferrer"}), which prevents comma-separated slices to be used when using the environment variable.

    A workaround is available where we instead put quotes around the environment variable value and replace the commas with spaces:

    ```
    WATCHTOWER_NOTIFICATIONS="slack msteams"
    ```

    If you're a `docker-compose` user, make sure to specify environment variables' values in your `.yml` file without double quotes (`"`).
    This prevents unexpected errors when Watchtower starts.

### Startup Notifications

Watchtower will log and send a notification every time it is started.

This behavior can be disabled with the [`DISABLE STARTUP MESSAGE`](../../configuration/arguments/index.md#disable_startup_message) configuration option.

## General Notification Settings

### Level

Controls the log level for notifications.

Possible values: `panic`, `fatal`, `error`, `warn`, `info`, `debug`, `trace`.

```text
            Argument: --notifications-level
Environment Variable: WATCHTOWER_NOTIFICATIONS_LEVEL
                Type: String
             Default: info
```

!!! Note
    In legacy mode (`--notification-report=false`), only `info`-level logs trigger notifications, ensuring a focused step-by-step update summary.

### Hostname

Custom hostname specified in subject/title.
Useful for overriding the operating system hostname.

```text
            Argument: --notifications-hostname
Environment Variable: WATCHTOWER_NOTIFICATIONS_HOSTNAME
                Type: String
             Default: None
```

### Delay

Delay before sending notifications expressed in seconds.

```text
            Argument: --notifications-delay
Environment Variable: WATCHTOWER_NOTIFICATIONS_DELAY
                Type: Integer
             Default: None
```

### Title Tag

Prefix to include in the title.
Useful when running multiple Watchtower instances.

```text
            Argument: --notification-title-tag
Environment Variable: WATCHTOWER_NOTIFICATION_TITLE_TAG
                Type: String
             Default: None
```

### Skip Title

Used to not pass the title param to notifications.
This will not pass a dynamic title override to notification services.
If no title is configured for the service, it will remove the title altogether.

```text
            Argument: --notification-skip-title
Environment Variable: WATCHTOWER_NOTIFICATION_SKIP_TITLE
                Type: Boolean
             Default: false
```

### Log Stdout

Enable output from `logger://` Shoutrrr service to stdout.

```text
            Argument: --notification-log-stdout
Environment Variable: WATCHTOWER_NOTIFICATION_LOG_STDOUT
                Type: Boolean
             Default: false
```

### Split by Container

Send separate notifications for each updated container instead of grouping them.

```text
            Argument: --notification-split-by-container
Environment Variable: WATCHTOWER_NOTIFICATION_SPLIT_BY_CONTAINER
                Type: Boolean
             Default: false
```

!!! Note
    When disabled (default), notifications are grouped for all updated containers in a single session.
    When enabled, a separate notification is sent for each container update.

#### Usage Example

To enable separate notifications per container:

```bash
docker run -d \
  --name watchtower \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_NOTIFICATIONS=slack \
  -e WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL="https://hooks.slack.com/services/xxx/yyyyyyyyyyyyyyy" \
  -e WATCHTOWER_NOTIFICATION_SPLIT_BY_CONTAINER=true \
  nickfedor/watchtower
```

## Email Notifications

Watchtower uses Shoutrrr's [smtp service](https://shoutrrr.nickfedor.com/services/email/){target="_blank" rel="noopener noreferrer"} to send email notifications.

Either legacy email notification flags or Shoutrrr URLs can be used; however, directly using URLs is recommended for greater control and clarity, especially for configuring TLS settings (e.g., STARTTLS or Implicit TLS).

To send notifications via e-mail, add `email` to the `--notifications` option or the `WATCHTOWER_NOTIFICATIONS` environment variable.

Email notification flags (e.g., `WATCHTOWER_NOTIFICATION_EMAIL_SERVER`, `WATCHTOWER_NOTIFICATION_EMAIL_FROM`) are automatically converted to a Shoutrrr SMTP URL internally.

### Common SMTP Configurations

=== "Gmail"

    | Property    | Value         |
    |-------------|---------------|
    | Port        | `587`         |
    | Encryption  | `ExplicitTLS` |
    | UseStartTLS | `Yes`         |

    ```text
    smtp://${USER}:${PASSWORD}@smtp.gmail.com:587/?fromaddress=${FROM}&toaddresses=${TO}&encryption=ExplicitTLS&usestarttls=yes&timeout=30s
    ```

    !!! Note
        For Gmail, use an [App Password](https://support.google.com/accounts/answer/185833){target="_blank" rel="noopener noreferrer"} if two-factor authentication is enabled.

=== "AWS SES"

    | Property    | Value         |
    |-------------|---------------|
    | Port        | `587`         |
    | Encryption  | `ExplicitTLS` |
    | UseStartTLS | `Yes`         |

    ```text
    smtp://${USER}:${PASSWORD}@email-smtp.us-east-1.amazonaws.com:587/?fromaddress=${FROM}&toaddresses=${TO}&encryption=ExplicitTLS&usestarttls=yes&timeout=30s
    ```

=== "Microsoft 365"

    | Property    | Value         |
    |-------------|---------------|
    | Port        | `587`         |
    | Encryption  | `ExplicitTLS` |
    | UseStartTLS | `Yes`         |

    ```text
    smtp://${USER}:${PASSWORD}@smtp.office365.com:587/?fromaddress=${FROM}&toaddresses=${TO}&encryption=ExplicitTLS&usestarttls=yes&timeout=30s
    ```

=== "Generic (SSL)"

    | Property    | Value         |
    |-------------|---------------|
    | Port        | `465`         |
    | Encryption  | `ImplicitTLS` |
    | UseStartTLS | `No`          |

    ```text
    smtp://${USER}:${PASSWORD}@smtp.example.com:465/?fromaddress=${FROM}&toaddresses=${TO}&encryption=ImplicitTLS&usestarttls=no&timeout=30s
    ```

=== "Generic (Plain)"

    | Property    | Value  |
    |-------------|--------|
    | Port        | `25`   |
    | Encryption  | `None` |
    | UseStartTLS | `No`   |

    ```text
    smtp://${USER}:${PASSWORD}@smtp.example.com:25/?fromaddress=${FROM}&toaddresses=${TO}&encryption=None&usestarttls=no&timeout=30s
    ```

#### Notes
<!-- markdownlint-disable -->
- **Timeout**:

    * The default SMTP timeout is 10 seconds.
    * If you experience timeouts (e.g., `failed to send: timed out: using smtp`), add `&timeout=30s` to the URL to allow more time for server responses, especially with proxies or slow networks.

- **Authentication**:

    * Use `&auth=Plain` for username/password authentication (default if credentials provided).
    * For OAuth2 (e.g., Gmail with app-specific passwords), use `&auth=OAuth2`.

- **Testing**:

    * Install Shoutrrr using one of the various [installation methods](https://shoutrrr.nickfedor.com/installation/){target="_blank" rel="noopener noreferrer"}.
    * Test your URL with the Shoutrrr CLI:
      ```bash
      shoutrrr send -u <URL> -m "Test message"
      ```

- **Proxy Issues**:

    * If using a Docker proxy (e.g., `tcp://dockerproxy:2375`), ensure it allows outbound connections to `${SMTP_HOST}:${SMTP_PORT}`.
    * Test connectivity with `telnet ${SMTP_HOST} ${SMTP_PORT}` inside the container.
<!-- markdownlint-restore -->

### Example Legacy Email Configuration

=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATIONS=email \
      -e WATCHTOWER_NOTIFICATION_EMAIL_FROM=sender@example.com \
      -e WATCHTOWER_NOTIFICATION_EMAIL_TO=recipient@example.com \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER=smtp.example.com \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT=587 \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER=user \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD=secret \
      -e WATCHTOWER_NOTIFICATION_EMAIL_DELAY=10 \
      nickfedor/watchtower
    ```

=== "Docker CLI (SMTP Relay)"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATIONS=email \
      -e WATCHTOWER_NOTIFICATION_EMAIL_FROM=sender@example.com \
      -e WATCHTOWER_NOTIFICATION_EMAIL_TO=recipient@example.com \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER=relay.example.com \
      nickfedor/watchtower
    ```

    !!! Note
        This assumes that you already have an SMTP server up and running that you can connect to.
        If you don't or you want to bring up Watchtower with your own simple SMTP relay, then check out the Docker Compose example.

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATIONS: email
          WATCHTOWER_NOTIFICATION_EMAIL_FROM: sender@example.com
          WATCHTOWER_NOTIFICATION_EMAIL_TO: recipient@example.com
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER: smtp.example.com
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT: 587
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER: user
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD: secret
          WATCHTOWER_NOTIFICATION_EMAIL_DELAY: 10
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

=== "Docker Compose (SMTP Relay)"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATIONS: email
          WATCHTOWER_NOTIFICATION_EMAIL_FROM: sender@example.com
          WATCHTOWER_NOTIFICATION_EMAIL_TO: recipient@example.com
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER: relay.example.com
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

    !!! Note
        The example assumes that your domain is called `example.com` and that you are going to use a valid certificate for `smtp.example.com`.

        This hostname has to be used as `WATCHTOWER_NOTIFICATION_EMAIL_SERVER`, otherwise the TLS connection will fail with `Failed to send notification email` or `connection: connection refused` errors.

        We also have to add a network for this setup in order to add an alias to it.

        If you also want to enable DKIM or other features on the SMTP server, then you will find more information at [freinet/postfix-relay](https://hub.docker.com/r/freinet/postfix-relay){target="_blank" rel="noopener noreferrer"}

### Legacy Notification Flags

#### Email From

The e-mail address from which notifications will be sent.

```text
            Argument: --notification-email-from
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_FROM
                Type: String
             Default: None
```

#### Email To

The e-mail address to which notifications will be sent.

```text
            Argument: --notification-email-to
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_TO
                Type: String
             Default: None
```

#### Email Server

The SMTP server (IP or FQDN) to send notifications through.

```text
            Argument: --notification-email-server
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_SERVER
                Type: String
             Default: None
```

#### Email Server TLS Skip Verify

Skip verification of the server certificate when using TLS.

```text
            Argument: --notification-email-server-tls-skip-verify
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_SERVER_TLS_SKIP_VERIFY
                Type: Boolean
             Default: false
```

#### Email Server User

The username for the SMTP server if it requires authentication.

```text
            Argument: --notification-email-server-user
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER
                Type: String
             Default: None
```

#### Email Server Password

The password for the SMTP server if it requires authentication.

```text
            Argument: --notification-email-server-password
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD
                Type: String
             Default: None
```

!!! Note
    This option can also reference a file, in which case the contents of the file are used.

#### Email Subject Tag

Subject prefix tag for notifications via mail.

```text
            Argument: --notification-email-subjecttag
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG
                Type: String
             Default: ""
```

#### Email Server Port

The port the SMTP server listens on.

```text
            Argument: --notification-email-server-port
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT
                Type: Integer
             Default: 25
```

#### Email Delay

The delay (in seconds) between sending notifications if multiple containers are updated at once.

```text
            Argument: --notification-email-delay
Environment Variable: WATCHTOWER_NOTIFICATION_EMAIL_DELAY
                Type: Integer
             Default: None
```

### Transitioning from Legacy Email Notifications to Shoutrrr

Watchtower includes a `watchtower notify-upgrade` command to convert legacy flags to a Shoutrrr URL.

The output is written to a temporary file, which you can copy using:

```bash
docker cp <CONTAINER>:<FILE_PATH> ./watchtower-notifications.env
```

#### Example Walkthrough

Example Legacy Configuration:

=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATIONS=email \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER=smtp.example.com \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT=587 \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER=user@example.com \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD=secret \
      -e WATCHTOWER_NOTIFICATION_EMAIL_FROM=sender@example.com \
      -e WATCHTOWER_NOTIFICATION_EMAIL_TO=recipient@example.com \
      nickfedor/watchtower
    ```

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATIONS: email
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER: smtp.example.com
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT: 587
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER: user@example.com
          WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD: secret
          WATCHTOWER_NOTIFICATION_EMAIL_FROM: sender@example.com
          WATCHTOWER_NOTIFICATION_EMAIL_TO: recipient@example.com
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

1. Run the following CLI command:

    ```bash
    docker compose exec watchtower watchtower notify-upgrade
    ```

    Converted Shoutrrr URL:

      ```text
      smtp://user@example.com:secret@smtp.example.com:587/?fromaddress=sender@example.com&toaddresses=recipient@example.com&encryption=ExplicitTLS&usestarttls=yes
      ```

2. Replace the legacy flags with:
<!-- markdownlint-disable -->
=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATIONS=shoutrrr \
      -e WATCHTOWER_NOTIFICATION_URL=smtp://user@example.com:secret@smtp.example.com:587/?fromaddress=sender@example.com&toaddresses=recipient@example.com&encryption=ExplicitTLS&usestarttls=yes \
      -e WATCHTOWER_NOTIFICATION_EMAIL_DELAY=10 \
      -e WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG=Watchtower \
      nickfedor/watchtower
    ```

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATIONS: shoutrrr
          WATCHTOWER_NOTIFICATION_URL: smtp://user@example.com:secret@smtp.example.com:587/?fromaddress=sender@example.com&toaddresses=recipient@example.com&encryption=ExplicitTLS&usestarttls=yes
          WATCHTOWER_NOTIFICATION_EMAIL_DELAY: "10"
          WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG: Watchtower
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```
<!-- markdownlint-restore -->
!!! Note
    Avoid using unrecognized flags like `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_SSL`, as they are ignored and may cause confusion.

    Use `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_TLS_SKIP_VERIFY` to disable TLS verification if needed (not recommended for production).

## Slack Notifications

### Example Slack Configuration

=== "Docker CLI"

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

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATIONS: slack
          WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL: "https://hooks.slack.com/services/xxx/yyyyyyyyyyyyyyy"
          WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER: watchtower-server-1
          WATCHTOWER_NOTIFICATION_SLACK_CHANNEL: "#my-custom-channel"
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

### Slack Configuration Options

To receive notifications in Slack, add `slack` to the `--notifications` option or the `WATCHTOWER_NOTIFICATIONS` environment variable.

Watchtower supports the following Slack-related options:

#### Slack Hook URL

The Slack webhook URL for notifications.

```text
            Argument: --notification-slack-hook-url
Environment Variable: WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL
                Type: String
             Default: None
```

!!! Note
    This option can also reference a file, in which case the contents of the file are used.

#### Slack Identifier

Custom name under which messages are sent.

```text
            Argument: --notification-slack-identifier
Environment Variable: WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER
                Type: String
             Default: watchtower
```

#### Slack Channel

A string which overrides the webhook's default channel (optional).

```text
            Argument: --notification-slack-channel
Environment Variable: WATCHTOWER_NOTIFICATION_SLACK_CHANNEL
                Type: String
             Default: None
```

## Microsoft Teams Notifications

=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATIONS=msteams \
      -e WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL="https://outlook.office.com/webhook/xxxxxxxx@xxxxxxx/IncomingWebhook/yyyyyyyy/zzzzzzzzzz" \
      -e WATCHTOWER_NOTIFICATION_MSTEAMS_USE_LOG_DATA=true \
      nickfedor/watchtower
    ```

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATIONS: msteams
          WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL: "https://outlook.office.com/webhook/xxxxxxxx@xxxxxxx/IncomingWebhook/yyyyyyyy/zzzzzzzzzz"
          WATCHTOWER_NOTIFICATION_MSTEAMS_USE_LOG_DATA: true
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

### Microsoft Teams Configuration Options

To receive notifications in Microsoft Teams, add `msteams` to the `--notifications` option or the `WATCHTOWER_NOTIFICATIONS` environment variable.

Watchtower supports the following Microsoft Teams-related options:

#### MSTeams Hook URL

The Microsoft Teams webhook URL for notifications.

```text
            Argument: --notification-msteams-hook
Environment Variable: WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL
                Type: String
             Default: None
```

!!! Note
    This option can also reference a file, in which case the contents of the file are used.

#### MSTeams Use Log Data

Enable sending keys/values filled by `log.WithField` or `log.WithFields` as Microsoft Teams message facts.

```text
            Argument: --notification-msteams-data
Environment Variable: WATCHTOWER_NOTIFICATION_MSTEAMS_USE_LOG_DATA
                Type: Boolean
             Default: false
```

## Gotify Notifications

=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATIONS=gotify \
      -e WATCHTOWER_NOTIFICATION_GOTIFY_URL="https://my.gotify.tld/" \
      -e WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN="SuperSecretToken" \
      -e WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY=true \
      nickfedor/watchtower
    ```

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATIONS: gotify
          WATCHTOWER_NOTIFICATION_GOTIFY_URL: "https://my.gotify.tld/"
          WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN: "SuperSecretToken"
          WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY: true
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

### Gotify Configuration Options

To push a notification to your Gotify instance, add `gotify` to the `--notifications` option or the `WATCHTOWER_NOTIFICATIONS` environment variable.

Watchtower supports the following Gotify-related options:

#### Gotify URL

The URL of the Gotify instance.

```text
            Argument: --notification-gotify-url
Environment Variable: WATCHTOWER_NOTIFICATION_GOTIFY_URL
                Type: String
             Default: None
```

#### Gotify Token

The app token for the Gotify instance.

```text
            Argument: --notification-gotify-token
Environment Variable: WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN
                Type: String
             Default: None
```

!!! Note
    This option can also reference a file, in which case the contents of the file are used.

#### Gotify TLS Skip Verify

Skip verification of the server certificate when using TLS.

```text
            Argument: --notification-gotify-tls-skip-verify
Environment Variable: WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY
                Type: Boolean
             Default: false
```

## Signal Notifications

Watchtower uses Shoutrrr's [signal service](https://shoutrrr.nickfedor.com/services/signal/){target="_blank" rel="noopener noreferrer"} to send Signal notifications.

Signal notifications require a Signal API server that can send messages on behalf of a registered Signal account. This is typically done using [signal-cli-rest-api](https://github.com/bbernhard/signal-cli-rest-api){target="_blank" rel="noopener noreferrer"} or [secured-signal-api](https://github.com/codeshelldev/secured-signal-api){target="_blank" rel="noopener noreferrer"}.

### Setting up Signal API Server

1. **Phone Number**: A dedicated phone number registered with Signal
2. **API Server**: A server running signal-cli with REST API capabilities
3. **Account Linking**: Linking the server as a secondary device to your Signal account
4. **Optional Security Layer**: Authentication and endpoint restrictions via a proxy

The server must be able to receive SMS verification codes during initial setup and maintain a persistent connection to Signal's servers.

### Example Signal Configuration

=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATION_URL=signal://localhost:8080/+1234567890/+0987654321 \
      nickfedor/watchtower
    ```

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATION_URL: signal://localhost:8080/+1234567890/+0987654321
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

### Signal URL Format

```
signal://[user:password@]host:port/source_phone/recipient1/recipient2
```

#### Parameters

- `host`: Signal API server hostname or IP address
- `port`: Signal API server port (default: 8080)
- `user`: Username for HTTP Basic Authentication (optional)
- `password`: Password for HTTP Basic Authentication (optional)
- `source_phone`: Your Signal phone number with country code (e.g., +1234567890)
- `recipient1, recipient2`: Phone numbers or group IDs to send to

#### TLS Configuration

- Use `signal://` for HTTPS (default, recommended)
- Use `signal://...?disabletls=yes` for HTTP (insecure, for local testing only)

#### Examples

Send to a single phone number:

```
signal://localhost:8080/+1234567890/+0987654321
```

Send to multiple recipients:

```
signal://localhost:8080/+1234567890/+0987654321/+1123456789/group.testgroup
```

Send to a group:

```
signal://localhost:8080/+1234567890/group.abcdefghijklmnop=
```

With authentication:

```
signal://user:password@localhost:8080/+1234567890/+0987654321
```

With API token (Bearer auth):

```
signal://localhost:8080/+1234567890/+0987654321?token=YOUR_API_TOKEN
```

Using HTTP instead of HTTPS:

```
signal://localhost:8080/+1234567890/+0987654321?disabletls=yes
```

### Signal Attachments

The Signal service supports sending base64-encoded attachments:

```bash
shoutrrr send "signal://localhost:8080/+1234567890/+0987654321" \
  "Message with attachment" \
  --attachments "base64data1,base64data2"
```

!!! Note
    Attachments must be provided as base64-encoded data. The API server handles MIME type detection and file handling.
