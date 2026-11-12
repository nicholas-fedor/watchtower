# Migration Tool

## Overview

Watchtower includes a `notify-upgrade` command to help convert legacy notification configurations to Shoutrrr URLs for use with the [`NOTIFICATION URL`](../../../configuration/notifications/index.md#notification_url).

It parses legacy Watchtower notification configurations and outputs Shoutrrr URLs to a temporary file inside the Watchtower container.

!!! Note "Settings the file leaves out"
    The file holds only the notification URL. It leaves out two legacy email settings, which are not part of a Shoutrrr URL. Watchtower applies both while your legacy settings are in place. When you switch to the generated URL, set their replacements yourself:

    - [`notification-email-delay`](../../../configuration/notifications/index.md#email_delay) is replaced by [`notifications-delay`](../../../configuration/notifications/index.md#notifications_delay).
    - [`notification-email-subjecttag`](../../../configuration/notifications/index.md#email_subject_tag) is replaced by [`notification-title-tag`](../../../configuration/notifications/index.md#notification_title_tag).

## Usage

The `notify-upgrade` command converts legacy Watchtower notification configurations into Shoutrrr URLs and writes them to a temporary file inside the container.

### Running the Command

The `notify-upgrade` can be run either against a Watchtower container that is already running or as a single-run instance that allows you to convert a Docker Compose configuration or Docker CLI flag / environment variable configuration.

#### Existing Deployment

If Watchtower is already deployed and running with your legacy notification configuration, you can execute `notify-upgrade` directly inside the container:

```bash
docker exec watchtower /watchtower notify-upgrade
```

This works because the Watchtower binary is installed at `/watchtower` and `notify-upgrade` is a built-in subcommand.
The common mistake is running `docker exec watchtower notify-upgrade`, which fails because `notify-upgrade` is not a standalone executable in `$PATH`.

!!! Note
    Running `notify-upgrade` inside an existing container does **not** stop or restart the main Watchtower process.
    `docker exec` runs a separate, short-lived process.
    The only side effect is that the `notify-upgrade` process will stay alive for up to 5 minutes while waiting for you to retrieve the temporary file, after which it cleans up and exits.

#### Single-Use Run

If you have not yet deployed Watchtower or if you want to run `notify-upgrade` as a one-off command without starting the persistent Watchtower update loop, then run a temporary container configured with `notify-upgrade` as the command.

=== "Email (SMTP)"

    === "Docker Compose"
        ```yaml
        services:
          watchtower:
            image: nickfedor/watchtower:latest
            command: notify-upgrade
            environment:
              WATCHTOWER_NOTIFICATIONS: email
              WATCHTOWER_NOTIFICATION_EMAIL_SERVER: smtp.example.com
              WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT: 587
              WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER: USERNAME
              WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD: PASSWORD
              WATCHTOWER_NOTIFICATION_EMAIL_FROM: sender@example.com
              WATCHTOWER_NOTIFICATION_EMAIL_TO: recipient@example.com
            volumes:
              - /var/run/docker.sock:/var/run/docker.sock
        ```
    === "Docker CLI (Env Vars)"
        ```bash
        docker run --rm \
          --name watchtower-notify-upgrade \
          -v /var/run/docker.sock:/var/run/docker.sock \
          -e WATCHTOWER_NOTIFICATIONS=email \
          -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER=smtp.example.com \
          -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PORT=587 \
          -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER=USERNAME \
          -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD=PASSWORD \
          -e WATCHTOWER_NOTIFICATION_EMAIL_FROM=sender@example.com \
          -e WATCHTOWER_NOTIFICATION_EMAIL_TO=recipient@example.com \
          nickfedor/watchtower \
          notify-upgrade
        ```
    === "Docker CLI (Flags)"
        ```bash
        docker run --rm \
          --name watchtower-notify-upgrade \
          -v /var/run/docker.sock:/var/run/docker.sock \
          nickfedor/watchtower \
          notify-upgrade \
          --notifications email \
          --notification-email-server smtp.example.com \
          --notification-email-server-port 587 \
          --notification-email-server-user USERNAME \
          --notification-email-server-password PASSWORD \
          --notification-email-from sender@example.com \
          --notification-email-to recipient@example.com
        ```

    Replace the following:

    - *`USERNAME`*: the user name for your SMTP server. `notify-upgrade` writes an `@` in it as `%40`.
    - *`PASSWORD`*: the password for your SMTP server

=== "Gotify"

    === "Docker Compose"
        ```yaml
        services:
          watchtower:
            image: nickfedor/watchtower:latest
            command: notify-upgrade
            environment:
              WATCHTOWER_NOTIFICATIONS: gotify
              WATCHTOWER_NOTIFICATION_GOTIFY_URL: "https://gotify.example.com/"
              WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN: "TOKEN"
              WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY: true
            volumes:
              - /var/run/docker.sock:/var/run/docker.sock
        ```
    === "Docker CLI (Env Vars)"
        ```bash
        docker run --rm \
          --name watchtower-notify-upgrade \
          -v /var/run/docker.sock:/var/run/docker.sock \
          -e WATCHTOWER_NOTIFICATIONS=gotify \
          -e WATCHTOWER_NOTIFICATION_GOTIFY_URL="https://gotify.example.com/" \
          -e WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN="TOKEN" \
          -e WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY=true \
          nickfedor/watchtower \
          notify-upgrade
        ```
    === "Docker CLI (Flags)"
        ```bash
        docker run --rm \
          --name watchtower-notify-upgrade \
          -v /var/run/docker.sock:/var/run/docker.sock \
          nickfedor/watchtower \
          notify-upgrade \
          --notifications gotify \
          --notification-gotify-url "https://gotify.example.com/" \
          --notification-gotify-token "TOKEN" \
          --notification-gotify-tls-skip-verify
        ```

    Replace *`TOKEN`* with the application token from your Gotify server.

=== "Slack"

    === "Docker Compose"
        ```yaml
        services:
          watchtower:
            image: nickfedor/watchtower:latest
            command: notify-upgrade
            environment:
              WATCHTOWER_NOTIFICATIONS: slack
              WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL: "https://hooks.slack.com/services/WORKSPACE_ID/WEBHOOK_ID/WEBHOOK_TOKEN"
              WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER: watchtower-server-1
            volumes:
              - /var/run/docker.sock:/var/run/docker.sock
        ```
    === "Docker CLI (Env Vars)"
        ```bash
        docker run --rm \
          --name watchtower-notify-upgrade \
          -v /var/run/docker.sock:/var/run/docker.sock \
          -e WATCHTOWER_NOTIFICATIONS=slack \
          -e WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL="https://hooks.slack.com/services/WORKSPACE_ID/WEBHOOK_ID/WEBHOOK_TOKEN" \
          -e WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER=watchtower-server-1 \
          nickfedor/watchtower \
          notify-upgrade
        ```
    === "Docker CLI (Flags)"
        ```bash
        docker run --rm \
          --name watchtower-notify-upgrade \
          -v /var/run/docker.sock:/var/run/docker.sock \
          nickfedor/watchtower \
          notify-upgrade \
          --notifications slack \
          --notification-slack-hook-url "https://hooks.slack.com/services/WORKSPACE_ID/WEBHOOK_ID/WEBHOOK_TOKEN" \
          --notification-slack-identifier watchtower-server-1 \
        ```

    Replace the following with the parts of your Slack webhook URL:

    - *`WORKSPACE_ID`*: the workspace ID, which starts with `T`
    - *`WEBHOOK_ID`*: the webhook ID, which starts with `B`
    - *`WEBHOOK_TOKEN`*: the webhook token

=== "Microsoft Teams"

    === "Docker Compose"
        ```yaml
        services:
          watchtower:
            image: nickfedor/watchtower:latest
            command: notify-upgrade
            environment:
              WATCHTOWER_NOTIFICATIONS: msteams
              WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL: "https://ENVIRONMENT.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/WORKFLOW_ID/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=SIGNATURE"
            volumes:
              - /var/run/docker.sock:/var/run/docker.sock
        ```
    === "Docker CLI (Env Vars)"
        ```bash
        docker run --rm \
          --name watchtower-notify-upgrade \
          -v /var/run/docker.sock:/var/run/docker.sock \
          -e WATCHTOWER_NOTIFICATIONS=msteams \
          -e WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL="https://ENVIRONMENT.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/WORKFLOW_ID/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=SIGNATURE" \
          nickfedor/watchtower \
          notify-upgrade
        ```
    === "Docker CLI (Flags)"
        ```bash
        docker run --rm \
          --name watchtower-notify-upgrade \
          -v /var/run/docker.sock:/var/run/docker.sock \
          nickfedor/watchtower \
          notify-upgrade \
          --notifications msteams \
          --notification-msteams-hook "https://ENVIRONMENT.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/WORKFLOW_ID/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=SIGNATURE"
        ```

    Replace the following with the parts of the webhook URL that your workflow's trigger shows:

    - *`ENVIRONMENT`*: the Power Platform environment
    - *`WORKFLOW_ID`*: the workflow ID
    - *`SIGNATURE`*: the value of the `sig` parameter

!!! Important
    Replace the example environment variables with your own legacy notification configuration.
    The `notify-upgrade` command inspects the container's current configuration, so it must be started with your legacy notification settings.

### Retrieving the Generated File

After running `notify-upgrade`, the converted Shoutrrr URL is written to a temporary file in the container's working directory. Its name starts with `watchtower-notif-urls-` and ends in a random number.

The command logs a copy command with the container and the file name:

```text
INFO To get the environment file, use: docker cp CONTAINER_ID:FILE_NAME ./watchtower-notifications.env
```

This output includes the following values:

- *`CONTAINER_ID`*: the ID of the container that ran `notify-upgrade`
- *`FILE_NAME`*: the name of the temporary file, such as `watchtower-notif-urls-1234567890`

Run that command to copy the file to your local machine:

```bash
docker cp CONTAINER_ID:FILE_NAME ./watchtower-notifications.env
```

The temporary file is automatically removed after **5 minutes** or when the container stops, so copy it promptly after generation.
If you are running `notify-upgrade` as a one-off command (`docker run --rm ...`), the process will wait up to 5 minutes before exiting.
Before it writes the file, `notify-upgrade` checks every URL with Shoutrrr, as Watchtower does at startup. If Shoutrrr rejects a URL, such as a Discord webhook whose ID is not a number, `notify-upgrade` writes no file. Its error never contains the URL. It usually names the service. It cannot when the URL has no scheme or does not parse. The same holds for a URL that ends in an empty `#` fragment. The error gives a notification URL's position, counting from 1. For a URL converted from a legacy notification type, it names that type instead. Shoutrrr's warnings about a URL, such as a deprecated key, appear in the log.
When the conversion fails, `notify-upgrade` logs the error and exits with code 1. It removes any file it started to write. If it logs "Failed to remove temporary file" instead, delete the file named in that warning yourself, because it can hold your notification credentials.

## Migration Walkthrough

Select the tab that matches your legacy notification service.

=== "Email (SMTP)"

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
                        WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER: USERNAME
                        WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD: PASSWORD
                        WATCHTOWER_NOTIFICATION_EMAIL_FROM: sender@example.com
                        WATCHTOWER_NOTIFICATION_EMAIL_TO: recipient@example.com
                        WATCHTOWER_NOTIFICATION_EMAIL_DELAY: 10
                        WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG: Watchtower
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
              -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_USER=USERNAME \
              -e WATCHTOWER_NOTIFICATION_EMAIL_SERVER_PASSWORD=PASSWORD \
              -e WATCHTOWER_NOTIFICATION_EMAIL_FROM=sender@example.com \
              -e WATCHTOWER_NOTIFICATION_EMAIL_TO=recipient@example.com \
              -e WATCHTOWER_NOTIFICATION_EMAIL_DELAY=10 \
              -e WATCHTOWER_NOTIFICATION_EMAIL_SUBJECTTAG=Watchtower \
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
              --notification-email-server-user USERNAME \
              --notification-email-server-password PASSWORD \
              --notification-email-from sender@example.com \
              --notification-email-to recipient@example.com \
              --notification-email-delay 10 \
              --notification-email-subjecttag Watchtower
            ```

        Replace the following:

        - *`USERNAME`*: the user name for your SMTP server. `notify-upgrade` writes an `@` in it as `%40`.
        - *`PASSWORD`*: the password for your SMTP server

    2. With the container running, execute the `notify-upgrade` command:

        ```bash
        docker exec watchtower /watchtower notify-upgrade
        ```

        Then copy the resulting file out of the container with the command that `notify-upgrade` logs:

        ```bash
        docker cp CONTAINER_ID:FILE_NAME ./watchtower-notifications.env
        ```

    3. The following converted Shoutrrr URL should be produced:

        ```text
        smtp://USERNAME:PASSWORD@smtp.example.com:587/?auth=Plain&clienthost=localhost&encryption=Auto&fromaddress=sender%40example.com&fromname=Watchtower&subject=&toaddresses=recipient%40example.com&usehtml=No&usestarttls=Yes&timeout=10s
        ```

    4. Replace the deprecated configuration with the converted Shoutrrr URL. The URL does not carry the email delay or the subject tag. Set the notifications delay and the title tag in their place:

        === "Docker Compose"
            ```yaml
            services:
                watchtower:
                    image: nickfedor/watchtower:latest
                    environment:
                        WATCHTOWER_NOTIFICATION_URL: smtp://USERNAME:PASSWORD@smtp.example.com:587/?auth=Plain&clienthost=localhost&encryption=Auto&fromaddress=sender%40example.com&fromname=Watchtower&subject=&toaddresses=recipient%40example.com&usehtml=No&usestarttls=Yes&timeout=10s
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
              -e "WATCHTOWER_NOTIFICATION_URL=smtp://USERNAME:PASSWORD@smtp.example.com:587/?auth=Plain&clienthost=localhost&encryption=Auto&fromaddress=sender%40example.com&fromname=Watchtower&subject=&toaddresses=recipient%40example.com&usehtml=No&usestarttls=Yes&timeout=10s" \
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
              --notification-url "smtp://USERNAME:PASSWORD@smtp.example.com:587/?auth=Plain&clienthost=localhost&encryption=Auto&fromaddress=sender%40example.com&fromname=Watchtower&subject=&toaddresses=recipient%40example.com&usehtml=No&usestarttls=Yes&timeout=10s" \
              --notifications-delay 10 \
              --notification-title-tag Watchtower
            ```

    !!! Note
        - Avoid using unrecognized flags like `WATCHTOWER_NOTIFICATION_EMAIL_SERVER_SSL`, as they are ignored and may cause confusion.
        - Use the `encryption` and `usestarttls` URL parameters in the `smtp://` URL to control TLS behavior rather than deprecated flags.

=== "Gotify"

    1. Run the following Docker Compose configuration / Docker CLI command:

        === "Docker Compose"
            ```yaml
            services:
                watchtower:
                    image: nickfedor/watchtower:latest
                    environment:
                        WATCHTOWER_NOTIFICATIONS: gotify
                        WATCHTOWER_NOTIFICATION_GOTIFY_URL: "https://gotify.example.com/"
                        WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN: "TOKEN"
                        WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY: true
                    volumes:
                    - /var/run/docker.sock:/var/run/docker.sock
            ```
        === "Docker CLI (Env Vars)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              -e WATCHTOWER_NOTIFICATIONS=gotify \
              -e WATCHTOWER_NOTIFICATION_GOTIFY_URL="https://gotify.example.com/" \
              -e WATCHTOWER_NOTIFICATION_GOTIFY_TOKEN="TOKEN" \
              -e WATCHTOWER_NOTIFICATION_GOTIFY_TLS_SKIP_VERIFY=true \
              nickfedor/watchtower
            ```
        === "Docker CLI (Flags)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              nickfedor/watchtower \
              --notifications gotify \
              --notification-gotify-url "https://gotify.example.com/" \
              --notification-gotify-token "TOKEN" \
              --notification-gotify-tls-skip-verify
            ```

        Replace *`TOKEN`* with the application token from your Gotify server.

    2. With the container running, execute the `notify-upgrade` command:

        ```bash
        docker exec watchtower /watchtower notify-upgrade
        ```

        Then copy the resulting file out of the container with the command that `notify-upgrade` logs:

        ```bash
        docker cp CONTAINER_ID:FILE_NAME ./watchtower-notifications.env
        ```

    3. The following converted Shoutrrr URL should be produced:

        ```text
        gotify://gotify.example.com/TOKEN?skiptlsverify=Yes&title=
        ```

    4. Replace the deprecated configuration with the converted Shoutrrr URL:

        === "Docker Compose"
            ```yaml
            services:
                watchtower:
                    image: nickfedor/watchtower:latest
                    environment:
                        WATCHTOWER_NOTIFICATION_URL: "gotify://gotify.example.com/TOKEN?skiptlsverify=Yes&title="
                    volumes:
                    - /var/run/docker.sock:/var/run/docker.sock
            ```
        === "Docker CLI (Env Vars)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              -e WATCHTOWER_NOTIFICATION_URL="gotify://gotify.example.com/TOKEN?skiptlsverify=Yes&title=" \
              nickfedor/watchtower
            ```
        === "Docker CLI (Flags)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              nickfedor/watchtower \
              --notification-url "gotify://gotify.example.com/TOKEN?skiptlsverify=Yes&title="
            ```

=== "Slack"

    1. Run the following Docker Compose configuration / Docker CLI command:

        === "Docker Compose"
            ```yaml
            services:
                watchtower:
                    image: nickfedor/watchtower:latest
                    environment:
                        WATCHTOWER_NOTIFICATIONS: slack
                        WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL: "https://hooks.slack.com/services/WORKSPACE_ID/WEBHOOK_ID/WEBHOOK_TOKEN"
                        WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER: watchtower-server-1
                    volumes:
                    - /var/run/docker.sock:/var/run/docker.sock
            ```
        === "Docker CLI (Env Vars)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              -e WATCHTOWER_NOTIFICATIONS=slack \
          -e WATCHTOWER_NOTIFICATION_SLACK_HOOK_URL="https://hooks.slack.com/services/WORKSPACE_ID/WEBHOOK_ID/WEBHOOK_TOKEN" \
          -e WATCHTOWER_NOTIFICATION_SLACK_IDENTIFIER=watchtower-server-1 \
              nickfedor/watchtower
            ```
        === "Docker CLI (Flags)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              nickfedor/watchtower \
              --notifications slack \
              --notification-slack-hook-url "https://hooks.slack.com/services/WORKSPACE_ID/WEBHOOK_ID/WEBHOOK_TOKEN" \
          --notification-slack-identifier watchtower-server-1 \
            ```

        Replace the following with the parts of your Slack webhook URL:

        - *`WORKSPACE_ID`*: the workspace ID, which starts with `T`
        - *`WEBHOOK_ID`*: the webhook ID, which starts with `B`
        - *`WEBHOOK_TOKEN`*: the webhook token

    2. With the container running, execute the `notify-upgrade` command:

        ```bash
        docker exec watchtower /watchtower notify-upgrade
        ```

        Then copy the resulting file out of the container with the command that `notify-upgrade` logs:

        ```bash
        docker cp CONTAINER_ID:FILE_NAME ./watchtower-notifications.env
        ```

    3. The following converted Shoutrrr URL should be produced:

        ```text
        slack://hook:WORKSPACE_ID-WEBHOOK_ID-WEBHOOK_TOKEN@webhook?botname=watchtower-server-1&color=%23406170
        ```

    4. Replace the deprecated configuration with the converted Shoutrrr URL:

        === "Docker Compose"
            ```yaml
            services:
                watchtower:
                    image: nickfedor/watchtower:latest
                    environment:
                        WATCHTOWER_NOTIFICATION_URL: "slack://hook:WORKSPACE_ID-WEBHOOK_ID-WEBHOOK_TOKEN@webhook?botname=watchtower-server-1&color=%23406170"
                    volumes:
                    - /var/run/docker.sock:/var/run/docker.sock
            ```
        === "Docker CLI (Env Vars)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              -e WATCHTOWER_NOTIFICATION_URL="slack://hook:WORKSPACE_ID-WEBHOOK_ID-WEBHOOK_TOKEN@webhook?botname=watchtower-server-1&color=%23406170" \
              nickfedor/watchtower
            ```
        === "Docker CLI (Flags)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              nickfedor/watchtower \
              --notification-url "slack://hook:WORKSPACE_ID-WEBHOOK_ID-WEBHOOK_TOKEN@webhook?botname=watchtower-server-1&color=%23406170"
            ```

=== "Microsoft Teams"

    1. Run the following Docker Compose configuration / Docker CLI command:

        === "Docker Compose"
            ```yaml
            services:
                watchtower:
                    image: nickfedor/watchtower:latest
                    environment:
                        WATCHTOWER_NOTIFICATIONS: msteams
                        WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL: "https://ENVIRONMENT.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/WORKFLOW_ID/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=SIGNATURE"
                    volumes:
                    - /var/run/docker.sock:/var/run/docker.sock
            ```
        === "Docker CLI (Env Vars)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              -e WATCHTOWER_NOTIFICATIONS=msteams \
              -e WATCHTOWER_NOTIFICATION_MSTEAMS_HOOK_URL="https://ENVIRONMENT.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/WORKFLOW_ID/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=SIGNATURE" \
              nickfedor/watchtower
            ```
        === "Docker CLI (Flags)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              nickfedor/watchtower \
              --notifications msteams \
              --notification-msteams-hook "https://ENVIRONMENT.environment.api.powerplatform.com:443/powerautomate/automations/direct/workflows/WORKFLOW_ID/triggers/manual/paths/invoke?api-version=1&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=SIGNATURE"
            ```

        Replace the following with the parts of the webhook URL that your workflow's trigger shows:

        - *`ENVIRONMENT`*: the Power Platform environment
        - *`WORKFLOW_ID`*: the workflow ID
        - *`SIGNATURE`*: the value of the `sig` parameter

    2. With the container running, execute the `notify-upgrade` command:

        ```bash
        docker exec watchtower /watchtower notify-upgrade
        ```

        Then copy the resulting file out of the container with the command that `notify-upgrade` logs:

        ```bash
        docker cp CONTAINER_ID:FILE_NAME ./watchtower-notifications.env
        ```

    3. The following converted Shoutrrr URL should be produced:

        ```text
        teams:?color=%23406170&host=https%3A%2F%2FENVIRONMENT.environment.api.powerplatform.com%3A443%2Fpowerautomate%2Fautomations%2Fdirect%2Fworkflows%2FWORKFLOW_ID%2Ftriggers%2Fmanual%2Fpaths%2Finvoke%3Fapi-version%3D1%26sp%3D%252Ftriggers%252Fmanual%252Frun%26sv%3D1.0%26sig%3DSIGNATURE
        ```

    4. Replace the deprecated configuration with the converted Shoutrrr URL:

        === "Docker Compose"
            ```yaml
            services:
                watchtower:
                    image: nickfedor/watchtower:latest
                    environment:
                        WATCHTOWER_NOTIFICATION_URL: "teams:?color=%23406170&host=https%3A%2F%2FENVIRONMENT.environment.api.powerplatform.com%3A443%2Fpowerautomate%2Fautomations%2Fdirect%2Fworkflows%2FWORKFLOW_ID%2Ftriggers%2Fmanual%2Fpaths%2Finvoke%3Fapi-version%3D1%26sp%3D%252Ftriggers%252Fmanual%252Frun%26sv%3D1.0%26sig%3DSIGNATURE"
                    volumes:
                    - /var/run/docker.sock:/var/run/docker.sock
            ```
        === "Docker CLI (Env Vars)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              -e WATCHTOWER_NOTIFICATION_URL="teams:?color=%23406170&host=https%3A%2F%2FENVIRONMENT.environment.api.powerplatform.com%3A443%2Fpowerautomate%2Fautomations%2Fdirect%2Fworkflows%2FWORKFLOW_ID%2Ftriggers%2Fmanual%2Fpaths%2Finvoke%3Fapi-version%3D1%26sp%3D%252Ftriggers%252Fmanual%252Frun%26sv%3D1.0%26sig%3DSIGNATURE" \
              nickfedor/watchtower
            ```
        === "Docker CLI (Flags)"
            ```bash
            docker run -d \
              --name watchtower \
              -v /var/run/docker.sock:/var/run/docker.sock \
              nickfedor/watchtower \
              --notification-url "teams:?color=%23406170&host=https%3A%2F%2FENVIRONMENT.environment.api.powerplatform.com%3A443%2Fpowerautomate%2Fautomations%2Fdirect%2Fworkflows%2FWORKFLOW_ID%2Ftriggers%2Fmanual%2Fpaths%2Finvoke%3Fapi-version%3D1%26sp%3D%252Ftriggers%252Fmanual%252Frun%26sv%3D1.0%26sig%3DSIGNATURE"
            ```

## Automated Migration Script

If you have a `docker-compose.yaml` with a `watchtower` service configured for legacy notifications, you can use the script below to run the entire migration process end-to-end. It starts the container, executes `notify-upgrade`, extracts the generated file, prints its contents, and cleans up.

```bash
#!/usr/bin/env bash
set -euo pipefail

SERVICE="${1:-watchtower}"
OUTPUT="${2:-./watchtower-notifications.env}"

CONTAINER=$(docker compose run -d "${SERVICE}" notify-upgrade)

trap 'docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true' EXIT

if ! docker ps -q --filter "id=${CONTAINER}" | grep -q .; then
    echo "notify-upgrade container exited before producing output. Logs:" >&2
    docker logs "${CONTAINER}" >&2 || true

    exit 1
fi

FILE=""
TIMEOUT=300
INTERVAL=2
ELAPSED=0

while [[ ${ELAPSED} -lt ${TIMEOUT} ]]; do
    FILE=$(docker logs "${CONTAINER}" 2>&1 | grep -oE 'watchtower-notif-urls-[^ ]+' | tail -1 || true)

    if [[ -n "${FILE}" ]]; then
        break
    fi

    sleep "${INTERVAL}"

    ELAPSED=$((ELAPSED + INTERVAL))
done

if [[ -z "${FILE}" ]]; then
    echo "Timed out waiting for notify-upgrade output. Logs:" >&2
    docker logs "${CONTAINER}" >/dev/null 2>&1 || true

    exit 1
fi

docker cp "${CONTAINER}:${FILE}" "${OUTPUT}"

echo "=== Generated Shoutrrr URL ==="
cat "${OUTPUT}"
```

!!! Important
    - The service must already be defined in `docker-compose.yaml` with your legacy notification environment variables. The script assumes the service name defaults to `watchtower`.
    - The command will self-cleanup after 5 minutes or when the script exits.
    - `notify-upgrade` keeps a Slack channel only when it is a Slack channel ID, such as `C0123456789`. A channel name such as `#ops` or a user such as `@dana` cannot be carried into the Shoutrrr URL, so the generated URL uses the webhook's own channel and `notify-upgrade` logs a warning. Webhooks created by a Slack app always post to their own channel.
