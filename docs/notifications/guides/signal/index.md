# Signal Notifications

## Overview

Watchtower uses Shoutrrr's [Signal service](https://shoutrrr.nickfedor.com/services/signal/){target="_blank" rel="noopener noreferrer"} to send Signal notifications.

Signal notifications require a Signal API server that can send messages on behalf of a registered Signal account.
This is typically done using [signal-cli-rest-api](https://github.com/bbernhard/signal-cli-rest-api){target="_blank" rel="noopener noreferrer"} or [secured-signal-api](https://github.com/codeshelldev/secured-signal-api){target="_blank" rel="noopener noreferrer"}.

## Examples

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATION_URL: signal://localhost:8080/+18005550100/+18005550101
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

=== "Docker CLI (Flags)"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      nickfedor/watchtower \
      --notification-url "signal://localhost:8080/+18005550100/+18005550101"
    ```

=== "Docker CLI (Env Vars)"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATION_URL=signal://localhost:8080/+18005550100/+18005550101 \
      nickfedor/watchtower
    ```

The examples send from `+18005550100` to `+18005550101` through a Signal API server on `localhost:8080`. [Shoutrrr's Signal URL Format](#shoutrrrs_signal_url_format) describes each part.

## Setting up a Signal API Server

1. **Phone Number**: A dedicated phone number registered with Signal
2. **API Server**: A server running signal-cli with REST API capabilities
3. **Account Linking**: Linking the server as a secondary device to your Signal account
4. **Optional Security Layer**: Authentication and endpoint restrictions via a proxy

The server must be able to receive SMS verification codes during initial setup and maintain a persistent connection to Signal's servers.

## Shoutrrr's Signal URL Format

```text
signal://[USERNAME:PASSWORD@]HOST[:PORT]/SOURCE_PHONE/RECIPIENT[/RECIPIENT...]
```

### Parameters

- *`USERNAME`*: the user name for HTTP Basic Authentication (optional)
- *`PASSWORD`*: the password for HTTP Basic Authentication (optional)
- *`HOST`*: the host name or IP address of the Signal API server
- *`PORT`*: the port of the Signal API server (default: 8080)
- *`SOURCE_PHONE`*: the Signal phone number that sends, with its country code, such as `+18005550100`
- *`RECIPIENT`*: a phone number, or a group ID written as `group.GROUP_ID`, to send to

### TLS Configuration

- Use `signal://` for HTTPS (default, recommended)
- Use `signal://...?disabletls=yes` for HTTP (insecure, for local testing only)

### Attachments

The Signal service supports sending base64-encoded attachments:

```bash
shoutrrr send "signal://localhost:8080/+18005550100/+18005550101" \
  "Message with attachment" \
  --attachments "ATTACHMENT_1,ATTACHMENT_2"
```

Replace *`ATTACHMENT_1`* and *`ATTACHMENT_2`* with the base64-encoded files to attach.

!!! Note
    Attachments must be provided as base64-encoded data. The API server handles MIME type detection and file handling.

### Examples

Send to a single phone number:

```
signal://localhost:8080/+18005550100/+18005550101
```

Send to multiple recipients:

```
signal://localhost:8080/+18005550100/+18005550101/+18005550102/group.GROUP_ID
```

Send to a group, replacing *`GROUP_ID`* with the group's ID:

```
signal://localhost:8080/+18005550100/group.GROUP_ID
```

With authentication, replacing *`USERNAME`* and *`PASSWORD`* with the HTTP Basic Authentication credentials:

```
signal://USERNAME:PASSWORD@localhost:8080/+18005550100/+18005550101
```

With API token (Bearer auth), replacing *`API_TOKEN`* with the token:

```
signal://localhost:8080/+18005550100/+18005550101?token=API_TOKEN
```

Using HTTP instead of HTTPS:

```
signal://localhost:8080/+18005550100/+18005550101?disabletls=yes
```
