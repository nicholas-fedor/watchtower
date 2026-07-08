# Migration Tool

## Overview

Watchtower includes a `notify-upgrade` command to help convert legacy notification configurations to Shoutrrr URLs for use with the [`NOTIFICATION URL`](../../../configuration/notifications/index.md#notification_url).

The output is written to a temporary file, which you can copy using:

```bash
docker cp <CONTAINER>:<FILE_PATH> ./watchtower-notifications.env
```

## SMTP Migration Walkthrough

Example Legacy Configuration:

1. Run the following Docker Compose configuration / Docker CLI command:

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
    === "Docker CLI (Env Vars)"
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
    === "Docker CLI (Flags)"
        ```bash
        docker run -d \
        --name watchtower \
        -v /var/run/docker.sock:/var/run/docker.sock \
        nickfedor/watchtower \
        --notifications email \
        --notification-email-server smtp.example.com \
        --notification-email-server-port 587 \
        --notification-email-server-user user@example.com \
        --notification-email-server-password secret \
        --notification-email-from sender@example.com \
        --notification-email-to recipient@example.com
        ```

2. The following converted Shoutrrr URL should be produced:

    ```text
    smtp://user@example.com:secret@smtp.example.com:587/?fromaddress=sender@example.com&toaddresses=recipient@example.com&encryption=ExplicitTLS&usestarttls=yes
    ```

3. Replace the deprecated configuration with the coverted Shoutrrr URL:

    === "Docker Compose"
        ```yaml
        services:
            watchtower:
                image: nickfedor/watchtower:latest
                environment:
                    WATCHTOWER_NOTIFICATION_URL: smtp://user@example.com:secret@smtp.example.com:587/?fromaddress=sender@example.com&toaddresses=recipient@example.com&encryption=ExplicitTLS&usestarttls=yes
                    WATCHTOWER_NOTIFICATIONS_DELAY: "10"
                    WATCHTOWER_NOTIFICATION_TITLE_TAG: Watchtower
                volumes:
                   - /var/run/docker.sock:/var/run/docker.sock
        ```
    === "Docker CLI (Env Vars)"
        ```bash
        docker run -d \
        --name watchtower \
        -v /var/run/docker.sock:/var/run/docker.sock \
        -e WATCHTOWER_NOTIFICATION_URL=smtp://user@example.com:secret@smtp.example.com:587/?fromaddress=sender@example.com&toaddresses=recipient@example.com&encryption=ExplicitTLS&usestarttls=yes \
        -e WATCHTOWER_NOTIFICATIONS_DELAY=10 \
        -e WATCHTOWER_NOTIFICATION_TITLE_TAG=Watchtower \
        nickfedor/watchtower
        ```
    === "Docker CLI (Flags)"
        ```bash
        docker run -d \
        --name watchtower \
        -v /var/run/docker.sock:/var/run/docker.sock \
        nickfedor/watchtower \
        --notification-url "smtp://user@example.com:secret@smtp.example.com:587/?fromaddress=sender@example.com&toaddresses=recipient@example.com&encryption=ExplicitTLS&usestarttls=yes" \
        --notifications-delay 10 \
        --notification-title-tag Watchtower
        ```

!!! Note
    - Avoid using unrecognized flags like `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_SSL`, as they are ignored and may cause confusion.
    - Use the `encryption` and `usestarttls` URL parameters in the `smtp://` URL to control TLS behavior rather than deprecated flags.
