# Microsoft Teams Notifications

!!! Warning "Deprecated Teams Notification Option"
    The following legacy Teams notification option is deprecated and subject to removal with the release of Watchtower v2:

    - [`notification-msteams-hook`](../../../configuration/notifications/index.md#microsoft_teams_hook)

    Use the [`notification-url`](../../../configuration/notifications/index.md#notification_url) configuration option with a `teams://` Shoutrrr URL instead.

## Overview

Watchtower can use Shoutrrr's [Teams service](https://shoutrrr.nickfedor.com/latest/services/chat/teams/){target="_blank" rel="noopener noreferrer"} to send [Microsoft Teams](https://www.microsoft.com/en-us/microsoft-teams){target="_blank" rel="noopener noreferrer"} notifications.

The webhook URL comes from a Power Automate workflow with the "When a Teams webhook request is received" trigger or from an Azure Logic Apps workflow. In Power Automate, set the trigger's "Who can trigger the flow?" option to "Anyone". Watchtower sends with the URL's `sig` signature and cannot sign in to your tenant. Copy the whole URL from the trigger.

Watchtower checks the webhook URL at startup through Shoutrrr, whether it is set in a `teams://` URL or in the legacy option. It must use `https://` and carry a `sig` signature. Its host must be one that Microsoft issues for workflows. Watchtower refuses to start with any other URL, such as a retired Office 365 connector URL on `webhook.office.com`. Shoutrrr's [webhook URL rules](https://shoutrrr.nickfedor.com/latest/services/chat/teams/#webhook_url){target="_blank" rel="noopener noreferrer"} list the accepted hosts.

!!! Note "Power Automate URLs on `logic.azure.com`"
    Power Automate flows left `logic.azure.com` on November 30, 2025. Their old URLs on that domain no longer trigger them. Copy the current URL from the flow's trigger.

    Watchtower still accepts `logic.azure.com` URLs because Azure Logic Apps workflows use that domain.

## Examples

In a `teams://` URL, the `host` value is the whole webhook URL, percent-encoded as in the examples below. Encode its percent signs too, so `%2F` becomes `%252F`. A raw `&` would end the `host` value early. The legacy option takes the webhook URL unencoded.

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: nickfedor/watchtower:latest
        environment:
          WATCHTOWER_NOTIFICATION_URL: "teams://?host=https%3A%2F%2FENVIRONMENT.environment.api.powerplatform.com%3A443%2Fpowerautomate%2Fautomations%2Fdirect%2Fworkflows%2FWORKFLOW_ID%2Ftriggers%2Fmanual%2Fpaths%2Finvoke%3Fapi-version%3D1%26sp%3D%252Ftriggers%252Fmanual%252Frun%26sv%3D1.0%26sig%3DSIGNATURE&title=Alert"
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
    ```

=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      nickfedor/watchtower \
      --notification-url "teams://?host=https%3A%2F%2FENVIRONMENT.environment.api.powerplatform.com%3A443%2Fpowerautomate%2Fautomations%2Fdirect%2Fworkflows%2FWORKFLOW_ID%2Ftriggers%2Fmanual%2Fpaths%2Finvoke%3Fapi-version%3D1%26sp%3D%252Ftriggers%252Fmanual%252Frun%26sv%3D1.0%26sig%3DSIGNATURE&title=Alert"
    ```

Replace the following with the parts of the webhook URL that your workflow's trigger shows:

- *`ENVIRONMENT`*: the Power Platform environment
- *`WORKFLOW_ID`*: the workflow ID
- *`SIGNATURE`*: the value of the `sig` parameter

/// details | The following legacy Microsoft Teams examples are deprecated and will be removed with the release of Watchtower v2.
    type: warning

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

///
