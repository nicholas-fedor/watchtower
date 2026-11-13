# Templates

## Overview

You can customize the message posted by setting a notification template.

### Notification Template

Sets the Go template used for formatting notification messages.

```text
            Argument: --notification-template
Environment Variable: WATCHTOWER_NOTIFICATION_TEMPLATE
                Type: String
             Default: See default templates below
```

### Notification Template File

Sets the path to a file containing the Go template used for formatting notification messages.

```text
            Argument: --notification-template-file
Environment Variable: WATCHTOWER_NOTIFICATION_TEMPLATE_FILE
                Type: String
             Default: (empty)
```

When both the [`notification-template`](#notification_template) and [`notification-template-file`](#notification_template_file) configuration options are specified, the file-based template takes precedence over the inline template.

#### Examples

Create a template file named `custom-template.txt` with your desired template content, then mount it into the container and specify the path:

=== "Docker Compose"

    ```yaml
    services:
        watchtower:
            image: nickfedor/watchtower:latest
            environment:
                WATCHTOWER_NOTIFICATION_TEMPLATE_FILE: "/custom-template.txt"
            volumes:
                - /var/run/docker.sock:/var/run/docker.sock
                - TEMPLATE_FILE:/custom-template.txt
    ```

    Replace *`TEMPLATE_FILE`* with the path of the template file on the Docker host. A relative path starts with `./` and is read from the Compose project directory, which is the directory of the first Compose file unless you set `--project-directory`.

=== "Docker CLI (Env Vars)"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -v "TEMPLATE_FILE:/custom-template.txt" \
      -e WATCHTOWER_NOTIFICATION_TEMPLATE_FILE="/custom-template.txt" \
      nickfedor/watchtower:latest
    ```

    Replace *`TEMPLATE_FILE`* with the absolute path of the template file on the Docker host.

### Notification Report

Enables the session report as the notification template data, including container statuses and logs.

```text
            Argument: --notification-report
Environment Variable: WATCHTOWER_NOTIFICATION_REPORT
                Type: Boolean
             Default: false
```

The template is a [Go template](https://golang.org/pkg/text/template/){target="_blank" rel="noopener noreferrer"} that processes either a list of log entries (`Message`, `Data`, `Level`, `Time`) captured from [zerolog](https://pkg.go.dev/github.com/rs/zerolog){target="_blank" rel="noopener noreferrer"} events or a `format.Data` struct, depending on the [`notification-report`](#notification_report) configuration option.

## Simple Templates

Simple templates are used when the [`notification-report`](#notification_report) configuration option is not set, formatting individual log entries as they occur.

```text title="Default Simple Template"
{{- range $i, $e := . -}}
{{- if $i}}{{- println -}}{{- end -}}
{{- $msg := $e.Message -}}
{{- if eq $msg "Found new image" -}}
    Found new image: {{$e.Data.image}} ({{with $e.Data.new_id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Stopping container" -}}
    Stopped stale container: {{$e.Data.container}} ({{with $e.Data.id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Started new container" -}}
    Started new container: {{$e.Data.container}} ({{with $e.Data.new_id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Removing image" -}}
    Removed stale image: {{with $e.Data.image_id}}{{.}}{{else}}unknown{{end}}
{{- else if eq $msg "Failed to list containers for image usage check, skipping removal" -}}
    Skipped image cleanup: {{with $e.Data.image_name}}{{.}}{{else}}unknown{{end}} ({{with $e.Data.image_id}}{{.}}{{else}}unknown{{end}}){{with $e.Data.error}}: {{.}}{{end}}
{{- else if eq $msg "Docker image usage exceeds configured maximum" -}}
    Docker image usage exceeds configured maximum: {{if HasKey $e.Data "usage"}}{{FormatDiskSpace (index $e.Data "usage")}}{{else}}unknown{{end}} of {{if HasKey $e.Data "max"}}{{FormatDiskSpace (index $e.Data "max")}}{{else}}unknown{{end}} used ({{if HasKey $e.Data "reclaimable"}}{{FormatDiskSpace (index $e.Data "reclaimable")}}{{else}}unknown{{end}} reclaimable, {{if HasKey $e.Data "image_count"}}{{index $e.Data "image_count"}}{{else}}unknown{{end}} images)
{{- else if eq $msg "Docker image usage exceeds configured warning threshold" -}}
    Docker image usage exceeds configured warning threshold: {{if HasKey $e.Data "usage"}}{{FormatDiskSpace (index $e.Data "usage")}}{{else}}unknown{{end}} of {{if HasKey $e.Data "warn"}}{{FormatDiskSpace (index $e.Data "warn")}}{{else}}unknown{{end}} used ({{if HasKey $e.Data "reclaimable"}}{{FormatDiskSpace (index $e.Data "reclaimable")}}{{else}}unknown{{end}} reclaimable, {{if HasKey $e.Data "image_count"}}{{index $e.Data "image_count"}}{{else}}unknown{{end}} images)
{{- else if eq $msg "Failed to query Docker image disk usage" -}}
    Failed to query Docker image disk usage{{with $e.Data.error}}: {{.}}{{end}}
{{- else if eq $msg "Docker image usage budget enabled" -}}
    Docker image usage budget enabled: maximum {{with $e.Data.disk_space_max}}{{FormatDiskSpace .}}{{else}}0 B{{end}}, warning at {{with $e.Data.disk_space_warn}}{{FormatDiskSpace .}}{{else}}0 B{{end}}
{{- else if $e.Data -}}
    {{$msg}} | {{range $k, $v := $e.Data -}}{{$k}}={{$v}} {{- end}}
{{- else -}}
    {{$msg}}
{{- end -}}
{{- end -}}
```

- This template processes `info`-level log entries in real-time, formatting key update events in past tense with container and image details from structured log fields.
- It sends each event immediately in legacy mode, mimicking a step-by-step log.

### Using Simple Templates in the Preview Tool

The [Template Preview Tool](../template-preview/index.md) uses the same template root as Watchtower:

- Report toggle off (legacy mode): the root is the log entry slice. Range over `.`, the same as the default simple template above.
- Report toggle on (report mode): the root is a `format.Data` value. Range over `.Entries` (and use `.Report` for session results).

!!! Note
    The example below is for report mode. With the report toggle off, use `range .` instead of `range .Entries`.

```text title="Preview example (report mode)"
{{- range $i, $e := .Entries -}}
{{- if $i}}{{- println -}}{{- end -}}
{{- $msg := $e.Message -}}
{{- if eq $msg "Found new image" -}}
    Found new image: {{$e.Data.image}} ({{with $e.Data.new_id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Stopping container" -}}
    Stopped stale container: {{$e.Data.container}} ({{with $e.Data.id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Started new container" -}}
    Started new container: {{$e.Data.container}} ({{with $e.Data.new_id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Removing image" -}}
    Removed stale image: {{with $e.Data.image_id}}{{.}}{{else}}unknown{{end}}
{{- else if eq $msg "Failed to list containers for image usage check, skipping removal" -}}
    Skipped image cleanup: {{with $e.Data.image_name}}{{.}}{{else}}unknown{{end}} ({{with $e.Data.image_id}}{{.}}{{else}}unknown{{end}}){{with $e.Data.error}}: {{.}}{{end}}
{{- else if eq $msg "Docker image usage exceeds configured maximum" -}}
    Docker image usage exceeds configured maximum: {{if HasKey $e.Data "usage"}}{{FormatDiskSpace (index $e.Data "usage")}}{{else}}unknown{{end}} of {{if HasKey $e.Data "max"}}{{FormatDiskSpace (index $e.Data "max")}}{{else}}unknown{{end}} used ({{if HasKey $e.Data "reclaimable"}}{{FormatDiskSpace (index $e.Data "reclaimable")}}{{else}}unknown{{end}} reclaimable, {{if HasKey $e.Data "image_count"}}{{index $e.Data "image_count"}}{{else}}unknown{{end}} images)
{{- else if eq $msg "Docker image usage exceeds configured warning threshold" -}}
    Docker image usage exceeds configured warning threshold: {{if HasKey $e.Data "usage"}}{{FormatDiskSpace (index $e.Data "usage")}}{{else}}unknown{{end}} of {{if HasKey $e.Data "warn"}}{{FormatDiskSpace (index $e.Data "warn")}}{{else}}unknown{{end}} used ({{if HasKey $e.Data "reclaimable"}}{{FormatDiskSpace (index $e.Data "reclaimable")}}{{else}}unknown{{end}} reclaimable, {{if HasKey $e.Data "image_count"}}{{index $e.Data "image_count"}}{{else}}unknown{{end}} images)
{{- else if eq $msg "Failed to query Docker image disk usage" -}}
    Failed to query Docker image disk usage{{with $e.Data.error}}: {{.}}{{end}}
{{- else if eq $msg "Docker image usage budget enabled" -}}
    Docker image usage budget enabled: maximum {{with $e.Data.disk_space_max}}{{FormatDiskSpace .}}{{else}}0 B{{end}}, warning at {{with $e.Data.disk_space_warn}}{{FormatDiskSpace .}}{{else}}0 B{{end}}
{{- else if $e.Data -}}
    {{$msg}} | {{range $k, $v := $e.Data -}}{{$k}}={{$v}} {{- end}}
{{- else -}}
    {{$msg}}
{{- end -}}
{{- end -}}
```

Example output for a log entry with `msg="Found new image"`:

```text
Found new image: repo/image:latest (abcdef123456)
```

## Report Templates

When the [`notification-report`](#notification_report) configuration option is set, the template processes a `format.Data` struct containing a session report and log entries.

```text title="Example Report Template"
{{- if .Report -}}
  {{- with .Report -}}
    {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Restarted}} Restarted, {{len .Failed}} Failed
    {{- if ( or .Updated .Restarted .Failed .Skipped ) -}}
      {{- range .Updated}}
- {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}{{with .GitRef}} ref {{.}}{{end}}{{with .Changelog}} {{.}}{{end}}
      {{- end -}}
      {{- range .Fresh}}
- {{.Name}} ({{.ImageName}}): {{.State}}
      {{- end -}}
      {{- range .Restarted}}
- {{.Name}} ({{.ImageName}}): {{.State}}
      {{- end -}}
      {{- range .Skipped}}
- {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
      {{- end -}}
      {{- range .Failed}}
- {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
{{- if .Entries -}}
  {{- if .Report }}

{{ end -}}
Logs:
  {{- range .Entries }}
{{ .Time.Format "2006-01-02T15:04:05Z07:00" }} [{{ .Level }}] {{ .Message }}
  {{- end -}}
{{- end -}}
```

- This example generates a summary of container statuses followed by logs, for notifications such as email or Slack messages.
- Some notifications carry no report. Their `.Report` is nil. Examples are a check request's messages and the messages that none of a split session's container notifications carries. A report template shows them only if it lists `.Entries`, as this example does.
- When the [`notification-split-by-container`](../general-settings/index.md#split_by_container) configuration option is enabled, `.Report.Focus` is the container the notification is about. It is nil for the report of a whole session, as in the [template preview](../template-preview/index.md). Read it inside `{{ with .Report }}`, because `.Report.Focus` fails the template when `.Report` is nil.
- The built-in `default` report template lists each container by its status instead of the logs. A container with a newer image that Watchtower did not apply is listed as `Stale`. Examples are a monitor-only container and a skipped Watchtower self-update.

### Example Usage
<!-- markdownlint-disable -->
=== "Docker Compose"

    ```yaml
    services:
        watchtower:
            image: nickfedor/watchtower:latest
            volumes:
                - /var/run/docker.sock:/var/run/docker.sock
            environment:
                WATCHTOWER_NOTIFICATION_REPORT: "true"
                WATCHTOWER_NOTIFICATION_URL: >
                    discord://DISCORD_WEBHOOK_TOKEN@DISCORD_WEBHOOK_ID
                    slack://hook:SLACK_WORKSPACE_ID-SLACK_WEBHOOK_ID-SLACK_WEBHOOK_TOKEN@webhook?botname=watchtower
                WATCHTOWER_NOTIFICATION_TEMPLATE: |
                    {{- if .Report -}}
                      {{- with .Report -}}
                        {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Restarted}} Restarted, {{len .Failed}} Failed
                        {{- if ( or .Updated .Restarted .Failed .Skipped ) -}}
                          {{- range .Updated}}
                    - {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}{{with .GitRef}} ref {{.}}{{end}}{{with .Changelog}} {{.}}{{end}}
                          {{- end -}}
                          {{- range .Fresh}}
                    - {{.Name}} ({{.ImageName}}): {{.State}}
                          {{- end -}}
                          {{- range .Restarted}}
                    - {{.Name}} ({{.ImageName}}): {{.State}}
                          {{- end -}}
                          {{- range .Skipped}}
                    - {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
                          {{- end -}}
                          {{- range .Failed}}
                    - {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
                          {{- end -}}
                        {{- end -}}
                      {{- end -}}
                    {{- end -}}
                    {{- if .Entries -}}
                      {{- if .Report }}

                    {{ end -}}
                    Logs:
                      {{- range .Entries }}
                    {{ .Time.Format "2006-01-02T15:04:05Z07:00" }} [{{ .Level }}] {{ .Message }}
                      {{- end -}}
                    {{- end -}}
    ```

    Replace the following:

    - *`DISCORD_WEBHOOK_TOKEN`*: the token of your Discord webhook
    - *`DISCORD_WEBHOOK_ID`*: the ID of your Discord webhook
    - *`SLACK_WORKSPACE_ID`*: the workspace ID of your Slack webhook URL, which starts with `T`
    - *`SLACK_WEBHOOK_ID`*: the webhook ID of your Slack webhook URL, which starts with `B`
    - *`SLACK_WEBHOOK_TOKEN`*: the webhook token of your Slack webhook URL

=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATION_REPORT="true" \
      -e WATCHTOWER_NOTIFICATION_URL="discord://DISCORD_WEBHOOK_TOKEN@DISCORD_WEBHOOK_ID" \
      -e WATCHTOWER_NOTIFICATION_TEMPLATE="\
    {{- if .Report -}}
      {{- with .Report -}}
        {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Restarted}} Restarted, {{len .Failed}} Failed
        {{- if ( or .Updated .Restarted .Failed .Skipped ) -}}
          {{- range .Updated}}
    - {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}{{with .GitRef}} ref {{.}}{{end}}{{with .Changelog}} {{.}}{{end}}
          {{- end -}}
          {{- range .Fresh}}
    - {{.Name}} ({{.ImageName}}): {{.State}}
          {{- end -}}
          {{- range .Restarted}}
    - {{.Name}} ({{.ImageName}}): {{.State}}
          {{- end -}}
          {{- range .Skipped}}
    - {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
          {{- end -}}
          {{- range .Failed}}
    - {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
          {{- end -}}
        {{- end -}}
      {{- end -}}
    {{- end -}}
    {{- if .Entries -}}
      {{- if .Report }}

    {{ end -}}
    Logs:
      {{- range .Entries }}
    {{ .Time.Format \"2006-01-02T15:04:05Z07:00\" }} [{{ .Level }}] {{ .Message }}
      {{- end -}}
    {{- end -}}
    " \
      nickfedor/watchtower:latest
    ```

    Replace the following:

    - *`DISCORD_WEBHOOK_TOKEN`*: the token of your Discord webhook
    - *`DISCORD_WEBHOOK_ID`*: the ID of your Discord webhook

<!-- markdownlint-restore -->
Example output for a session with one updated container, one restarted container, and one error log:

```text
5 Scanned, 1 Updated, 1 Restarted, 0 Failed
- container (repo/image:latest): abcdef123456 updated to 34567890abcd
- restarted-container (repo/image:latest): Restarted

Logs:
2025-08-20T06:00:13-07:00 [error] Operation failed. Try again later.
```

## Git and OCI report fields

Custom report templates can use these fields on each container.
They are populated even when Git monitoring is off.

| Field                   | Meaning                                                                                                                                                                  |
|:------------------------|:-------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `.GitRepo`              | `com.centurylinklabs.watchtower.git-repo`, else [`git-image`](../../configuration/git-monitoring/index.md#git_image) mapping, else OCI `org.opencontainers.image.source` |
| `.GitRef`               | `com.centurylinklabs.watchtower.git-ref` (or `com.centurylinklabs.watchtower.git-branch`), else mapping ref. Empty when the container is not Git-associated.             |
| `.Changelog`            | `com.centurylinklabs.watchtower.changelog-url`, else a derived releases URL, else OCI `url` / `documentation`                                                            |
| `.Source`               | OCI `org.opencontainers.image.source`                                                                                                                                    |
| `.ImageURL`             | OCI `org.opencontainers.image.url`                                                                                                                                       |
| `.Documentation`        | OCI `org.opencontainers.image.documentation`                                                                                                                             |
| `.CurrentImageVersion`  | OCI `org.opencontainers.image.version` of the running image                                                                                                              |
| `.LatestImageVersion`   | OCI `org.opencontainers.image.version` of the newly pulled image. Empty when there is no newer image.                                                                    |
| `.CurrentImageRevision` | OCI `org.opencontainers.image.revision` of the running image                                                                                                             |
| `.LatestImageRevision`  | OCI `org.opencontainers.image.revision` of the newly pulled image. Empty when there is no newer image.                                                                   |

The `Current*` fields describe the image the container is running, the `Latest*` fields the image it is moving to.
`.Changelog`, `.Source`, `.ImageURL`, and `.Documentation` are resolved from the **latest known** image, so they describe the new version once one is found and the running image otherwise.

`.GitRef` is the configured Git ref and nothing else.
An OCI version is never copied into it.
Use `.CurrentImageVersion` or `.LatestImageVersion` instead.

Because the two versions are separate fields, a template can print the transition:

```text
{{- with .Report -}}
  {{- range $i, $c := .Updated -}}
    {{- if $i }}
{{ end -}}
    {{ $c.Name }}: {{ $c.CurrentImageVersion }} → {{ $c.LatestImageVersion }}
    {{- with $c.Changelog }}
{{ . }}
    {{- end -}}
  {{- end -}}
{{- end -}}
```

```text
app: 1.2.2 → 1.2.3
https://github.com/org/app/releases/tag/v1.2.3
```

The built-in `default` report template prints image IDs, the ref, and the changelog on an updated container when they are set:

```text
- app (myapp:latest): abcdef123456 updated to 34567890abcd ref v1.2.4 https://github.com/org/app/releases
```

### Changelog resolution

1. `com.centurylinklabs.watchtower.changelog-url` label, with placeholders substituted.
2. A **versioned** releases URL, when the exact release tag is known. See [Versioned release links](#versioned_release_links).
3. An **unversioned** releases URL built for `github.com`, `gitlab.com`, `codeberg.org`, and for a container that sets `com.centurylinklabs.watchtower.git-host`.
4. OCI `org.opencontainers.image.url`.
5. OCI `org.opencontainers.image.documentation`.

An explicit `com.centurylinklabs.watchtower.changelog-url` label may include placeholders from the new version when known: `{major}`, `{minor}`, `{patch}`, `{tag}`, `{commit}`.

```yaml
labels:
    com.centurylinklabs.watchtower.git-repo: https://github.com/org/app.git
    com.centurylinklabs.watchtower.changelog-url: https://github.com/org/app/releases/tag/v{major}.{minor}.{patch}
```

Malformed or unfilled placeholders are left as-is.
The session does not fail.

An image with only `org.opencontainers.image.source` still exposes `.Source` / derived `.GitRepo`.
Git monitoring stays off unless the container is associated and the watcher is on.
See [Git Monitoring](../../advanced-features/git-monitoring/index.md) for association and rebuilds.

### Versioned release links

A derived changelog normally points at the **release index**, for example `https://github.com/org/app/releases`.
A versioned link such as `https://github.com/org/app/releases/tag/v1.2.3` is better, but the exact tag spelling cannot be derived from the OCI version alone. A release tag and the version annotation of the same release need not agree on a leading `v`.

To resolve it, Watchtower confirms which tag actually matches the image it pulled by comparing manifest digests, then uses that tag's exact spelling.

!!! Warning "This costs extra registry requests"
    Resolving the tag issues one or two extra manifest requests per updated container, which roughly doubles that session's manifest requests for the update.

    The lookup is therefore **off by default**. Enable it per deployment with [`--enable-changelog`](../../configuration/update-behavior/index.md#changelog_links) or the `com.centurylinklabs.watchtower.enable-changelog` label.

    A `changelog-url` label does **not** enable it. Setting a URL says what the link is, not that a registry request is authorized, so a template referencing `{tag}` stays unfilled until the option is turned on for that container.

With the option off, the changelog resolves to the unversioned release index and costs no requests.
A Git-monitored container never needs the lookup, because its tag comes from the semver policy rather than a registry.
Under a policy of `none` a branch-triggered rebuild resolves no tag, so a template referencing `{tag}` stays unfilled.
A container with no OCI version, a digest-pinned image, and an unknown or unparseable repo host all skip the lookup.
A rate limit on the first candidate abandons the probe rather than spend a second request the registry will refuse.
Results are cached per session, so sibling containers on the same image only pay once.

### Changelog in the legacy notification

When the option is enabled and a URL resolves, the `default-legacy` notification gains one line per updated container:

```text
Changelog: https://github.com/org/app/releases/tag/v1.2.3
```

Nothing is emitted when the option is off, so a default deployment's output is unchanged.

### Log lines from a Git update

With [notification report](../../configuration/notifications/index.md#notification_report) off, Watchtower sends the log line itself. The `default-legacy` template formats these messages instead of dumping every field:

| Message                                                                      | What the notification says                            |
|:-----------------------------------------------------------------------------|:------------------------------------------------------|
| `Found new Git revision`                                                     | `Found new Git revision: REVISION (SHORT_COMMIT)` |
| `Built image from Git URL context`                                           | `Built image: IMAGE (IMAGE_ID)` |
| `Built Compose project`                                                      | `Built Compose project: PROJECT (SERVICES)`, only when containers are not restarted |
| `Git build failed. Leaving running container untouched`                      | Container and the error. The running container was left in place. |
| `Compose apply failed. Docker Compose may have partially recreated services` | Container and a short failure line. |
| `Compose project directory is not readable. Leaving the running container untouched` | `Skipped CONTAINER: compose directory is not readable` |
| `Skipped container with an invalid git semver policy`                        | `Skipped CONTAINER: invalid git semver policy` |
| `Skipped container with an invalid git-host`                                 | `Skipped CONTAINER: invalid git-host` |

These lines include the following values:

- *`REVISION`*: the repository path and the tag or branch, such as `github.com/org/app@v1.2.3`
- *`SHORT_COMMIT`*: the first 12 characters of the commit
- *`IMAGE`*: the built image, such as `app:git-0123456789ab`
- *`IMAGE_ID`*: the short ID of the built image
- *`PROJECT`*: the Compose project
- *`SERVICES`*: the Compose services that were built, separated by commas
- *`CONTAINER`*: the container name

The log fields are `container`, `image`, `repo`, `ref`, `commit`, `changelog`, `project`, `dir`, and `error`. A custom simple template reads them from `.Data`:

```text
{{- $sep := "" -}}
{{- range . -}}
  {{- if eq .Message "Built image from Git URL context" -}}
    {{- $sep }}{{ .Data.container }} {{ .Data.commit }} {{ .Data.changelog }}
    {{- $sep = "\n" -}}
  {{- end -}}
{{- end -}}
```

The `json` notification report emits the same values under camelCase keys: `gitRepo`, `gitRef`, `changelog`, `ociSource`, `imageUrl`, `documentation`, `currentImageVersion`, `latestImageVersion`, `currentImageRevision`, and `latestImageRevision`.
Porcelain JSON and `/v1/check` use snake_case instead: `git_repo`, `git_ref`, `changelog`, `oci_source`, `image_url`, `documentation`, `current_image_version`, `latest_image_version`, `current_revision`, and `latest_revision`.

## Cooldown report fields

A container that an [image cooldown](../../advanced-features/image-cooldown/index.md) holds back is listed under `.Report.Skipped`.
Report templates can read the cooldown on it with these fields.
They are empty, or the zero time, on a container that no cooldown holds back.

| Field                 | Meaning                                                                                       |
|:----------------------|:----------------------------------------------------------------------------------------------|
| `.CooldownDelay`      | The configured cooldown, such as `1 day`                                                      |
| `.CooldownAge`        | Age of the newer image, such as `18 hours`. Empty when its age is unavailable.                |
| `.CooldownRemaining`  | Time until the image is old enough, such as `6 hours`. Empty when its age is unavailable.     |
| `.CooldownEligibleAt` | Time the image is old enough, as a Go `time.Time`. The zero time when its age is unavailable. |

```text
{{- with .Report -}}
  {{- $sep := "" -}}
  {{- range .Skipped -}}
    {{- if .CooldownDelay -}}
      {{- $sep }}{{ .Name }}: cooldown {{ .CooldownDelay }}
      {{- with .CooldownRemaining }}, eligible in {{ . }}{{ end -}}
      {{- $sep = "\n" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
```

```text
app: cooldown 1 day, eligible in 6 hours
```

In the [template preview](../template-preview/index.md), the first skipped container is held back by a cooldown and carries these fields.

## Customizing Templates

You can create custom templates to format notifications differently.

Use the [Template Preview Tool](../template-preview/index.md) to test your templates interactively.

!!! Note
    When the preview report toggle is off, simple templates can range over `.` just as they do in Watchtower. When the report toggle is on, range over `.Entries`.

## Additional Resources

- For detailed template syntax, refer to the [Go Template documentation](https://golang.org/pkg/text/template/){target="_blank" rel="noopener noreferrer"}.
- For log entry fields, each entry exposes `Message`, `Data` (map of structured fields), `Level`, and `Time` (see the `Entry` type in `internal/observability/notify/format` and [zerolog](https://pkg.go.dev/github.com/rs/zerolog){target="_blank" rel="noopener noreferrer"}).
