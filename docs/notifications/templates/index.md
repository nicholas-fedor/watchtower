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
                - /path/to/custom-template.txt:/custom-template.txt
    ```

=== "Docker CLI (Env Vars)"

    ```bash
    docker run -d \
    --name watchtower \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v /path/to/custom-template.txt:/custom-template.txt \
    -e WATCHTOWER_NOTIFICATION_TEMPLATE_FILE="/custom-template.txt" \
    nickfedor/watchtower:latest
    ```

### Notification Report

Enables the session report as the notification template data, including container statuses and logs.

```text
            Argument: --notification-report
Environment Variable: WATCHTOWER_NOTIFICATION_REPORT
                Type: Boolean
             Default: false
```

The template is a [Go template](https://golang.org/pkg/text/template/){target="_blank" rel="noopener noreferrer"} that processes either a list of log entries (`Message`, `Data`, `Level`, `Time`) captured from [zerolog](https://pkg.go.dev/github.com/rs/zerolog){target="_blank" rel="noopener noreferrer"} events or a `notifications.Data` struct, depending on the [`notification-report`](#notification_report) configuration option.

## Simple Templates

Simple templates are used when the [`notification-report`](#notification_report) configuration option is not set, formatting individual log entries as they occur.

```go title="Default Simple Template"
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
{{- else if eq $msg "Detected multiple Watchtower instances - initiating cleanup" -}}
    Detected {{$e.Data.count}} Watchtower instances - initiating cleanup
{{- else if eq $msg "Docker image usage exceeds configured maximum" -}}
    Docker image usage exceeds configured maximum: {{with $e.Data.usage}}{{.}}{{else}}unknown{{end}}/{{with $e.Data.max}}{{.}}{{else}}unknown{{end}} bytes used (reclaimable {{with $e.Data.reclaimable}}{{.}}{{else}}unknown{{end}}, {{with $e.Data.image_count}}{{.}}{{else}}unknown{{end}} images)
{{- else if eq $msg "Docker image usage exceeds configured warning threshold" -}}
    Docker image usage exceeds configured warning threshold: {{with $e.Data.usage}}{{.}}{{else}}unknown{{end}}/{{with $e.Data.warn}}{{.}}{{else}}unknown{{end}} bytes used (reclaimable {{with $e.Data.reclaimable}}{{.}}{{else}}unknown{{end}}, {{with $e.Data.image_count}}{{.}}{{else}}unknown{{end}} images)
{{- else if eq $msg "Failed to query Docker image disk usage" -}}
    Failed to query Docker image disk usage{{with $e.Data.error}}: {{.}}{{end}}
{{- else if eq $msg "Docker image usage budget enabled" -}}
    Docker image usage budget enabled: max {{with $e.Data.disk_space_max}}{{.}}{{else}}0{{end}} bytes, warn {{with $e.Data.disk_space_warn}}{{.}}{{else}}0{{end}} bytes
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
- Report toggle on (report mode): the root is a `notifications.Data` value. Range over `.Entries` (and use `.Report` for session results).

!!! Note
    The example below is for report mode. With the report toggle off, use `range .` instead of `range .Entries`.

```go title="Preview example (report mode)"
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
{{- else if eq $msg "Detected multiple Watchtower instances - initiating cleanup" -}}
    Detected {{$e.Data.count}} Watchtower instances - initiating cleanup
{{- else if eq $msg "Docker image usage exceeds configured maximum" -}}
    Docker image usage exceeds configured maximum: {{with $e.Data.usage}}{{.}}{{else}}unknown{{end}}/{{with $e.Data.max}}{{.}}{{else}}unknown{{end}} bytes used (reclaimable {{with $e.Data.reclaimable}}{{.}}{{else}}unknown{{end}}, {{with $e.Data.image_count}}{{.}}{{else}}unknown{{end}} images)
{{- else if eq $msg "Docker image usage exceeds configured warning threshold" -}}
    Docker image usage exceeds configured warning threshold: {{with $e.Data.usage}}{{.}}{{else}}unknown{{end}}/{{with $e.Data.warn}}{{.}}{{else}}unknown{{end}} bytes used (reclaimable {{with $e.Data.reclaimable}}{{.}}{{else}}unknown{{end}}, {{with $e.Data.image_count}}{{.}}{{else}}unknown{{end}} images)
{{- else if eq $msg "Failed to query Docker image disk usage" -}}
    Failed to query Docker image disk usage{{with $e.Data.error}}: {{.}}{{end}}
{{- else if eq $msg "Docker image usage budget enabled" -}}
    Docker image usage budget enabled: max {{with $e.Data.disk_space_max}}{{.}}{{else}}0{{end}} bytes, warn {{with $e.Data.disk_space_warn}}{{.}}{{else}}0{{end}} bytes
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

When the [`notification-report`](#notification_report) configuration option is set, the template processes a `notifications.Data` struct containing a session report and log entries.

```go title="Default Report Template"
{{- if .Report -}}
  {{- with .Report -}}
    {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Restarted}} Restarted, {{len .Failed}} Failed
    {{- if ( or .Updated .Restarted .Failed ) -}}
      {{- range .Updated}}
- {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}
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
{{- if .Entries -}}

Logs:
{{- end -}}
{{range .Entries -}}{{.Time.Format "2006-01-02T15:04:05Z07:00"}} [{{.Level}}] {{.Message}}{{"\n"}}{{- end -}}
{{- end -}}
```

- This template generates a summary of container statuses (scanned, updated, failed, etc.) followed by logs, used for notifications like email or Slack messages.

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
                    discord://token@channel
                    slack://watchtower@token-a/token-b/token-c
                WATCHTOWER_NOTIFICATION_TEMPLATE: |
                    {{- if .Report -}}
                    {{- with .Report -}}
                    {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Restarted}} Restarted, {{len .Failed}} Failed
                    {{- if ( or .Updated .Restarted .Failed ) -}}
                        {{- range .Updated -}}
                    - {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}
                        {{- end -}}
                        {{- range .Fresh -}}
                    - {{.Name}} ({{.ImageName}}): {{.State}}
                        {{- end -}}
                        {{- range .Restarted -}}
                    - {{.Name}} ({{.ImageName}}): {{.State}}
                        {{- end -}}
                        {{- range .Skipped -}}
                    - {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
                        {{- end -}}
                        {{- range .Failed -}}
                    - {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
                        {{- end -}}
                    {{- end -}}
                    {{- end -}}
                    {{- if .Entries -}}

                    Logs:
                    {{- end -}}
                    {{- range .Entries -}}{{.Time.Format "2006-01-02T15:04:05Z07:00"}} [{{.Level}}] {{.Message}}{{"\n"}}{{- end -}}
                    {{- end -}}
    ```

=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATION_REPORT="true" \
      -e WATCHTOWER_NOTIFICATION_TEMPLATE="\
    {{- if .Report -}}
      {{- with .Report -}}
    {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Restarted}} Restarted, {{len .Failed}} Failed
    {{- if ( or .Updated .Restarted .Failed ) -}}
          {{- range .Updated -}}
    - {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}
          {{- end -}}
          {{- range .Fresh -}}
    - {{.Name}} ({{.ImageName}}): {{.State}}
          {{- end -}}
          {{- range .Restarted -}}
    - {{.Name}} ({{.ImageName}}): {{.State}}
          {{- end -}}
          {{- range .Skipped -}}
    - {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
          {{- end -}}
          {{- range .Failed -}}
    - {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
          {{- end -}}
    {{- end -}}
      {{- end -}}
    {{- if .Entries -}}

    Logs:
    {{- end -}}
    {{- range .Entries -}}{{.Time.Format \"2006-01-02T15:04:05Z07:00\"}} [{{.Level}}] {{.Message}}{{\"\n\"}}{{- end -}}
    {{- end -}}
    " \
      watchtower-image
    ```

<!-- markdownlint-restore -->
Example output for a session with one updated container, one restarted container, and one error log:

```text
5 Scanned, 1 Updated, 1 Restarted, 0 Failed
- /container (repo/image:latest): abcdef12 updated to 34567890
- /restarted-container (repo/image:latest): Restarted

Logs:
2025-08-20T06:00:13-07:00 [error] Operation failed. Try again later.
```

## Git and OCI report fields

Default templates are unchanged.
Custom report templates can use these fields on each container.
They are populated even when Git monitoring is off.

| Field | Meaning |
|:------|:--------|
| `.GitRepo` | `com.centurylinklabs.watchtower.git-repo`, else [`git-image`](../../configuration/git-monitoring/index.md#git_image) mapping, else OCI `org.opencontainers.image.source` |
| `.GitRef` | `com.centurylinklabs.watchtower.git-ref` (or `com.centurylinklabs.watchtower.git-branch`), else mapping ref, else OCI version when it looks like a tag |
| `.Changelog` | `com.centurylinklabs.watchtower.changelog`, else a derived releases URL, else OCI `url` / `documentation` |
| `.Source` | OCI `org.opencontainers.image.source` |
| `.ImageURL` | OCI `org.opencontainers.image.url` |
| `.Documentation` | OCI `org.opencontainers.image.documentation` |
| `.Revision` | OCI `org.opencontainers.image.revision` |

```go
{{ range .Report.Updated }}{{ .Name }} {{ .Changelog }}{{ if .Source }} ({{ .Source }}){{ end }}{{ end }}
```

An explicit `com.centurylinklabs.watchtower.changelog` label may include placeholders from the new version when known: `{major}`, `{minor}`, `{patch}`, `{tag}`, `{commit}`.

```yaml
labels:
    com.centurylinklabs.watchtower.git-repo: https://github.com/org/app.git
    com.centurylinklabs.watchtower.changelog: https://github.com/org/app/releases/tag/v{major}.{minor}.{patch}
```

If `com.centurylinklabs.watchtower.changelog` is unset, Watchtower builds a releases URL for `github.com`, `gitlab.com`, `codeberg.org`, and for a container that sets `com.centurylinklabs.watchtower.git-host`.
Otherwise it falls back to OCI `org.opencontainers.image.url`, then `org.opencontainers.image.documentation`.

Malformed placeholders are left as-is.
The session does not fail.

An image with only `org.opencontainers.image.source` still exposes `.Source` / derived `.GitRepo`.
Git monitoring stays off unless the container is associated and the watcher is on.
See [Git Monitoring](../../advanced-features/git-monitoring/index.md) for association and rebuilds.

Porcelain JSON and `/v1/check` expose the same values as `git_repo`, `git_ref`, `changelog`, `oci_source`, `image_url`, `documentation`, and `revision`.

## Customizing Templates

You can create custom templates to format notifications differently.

Use the [Template Preview Tool](../template-preview/index.md) to test your templates interactively.

!!! Note
    When the preview report toggle is off, simple templates can range over `.` just as they do in Watchtower. When the report toggle is on, range over `.Entries`.

## Additional Resources

- For detailed template syntax, refer to the [Go Template documentation](https://golang.org/pkg/text/template/){target="_blank" rel="noopener noreferrer"}.
- For log entry fields, each entry exposes `Message`, `Data` (map of structured fields), `Level`, and `Time` (see `pkg/notifications` notification entries and [zerolog](https://pkg.go.dev/github.com/rs/zerolog){target="_blank" rel="noopener noreferrer"}).
