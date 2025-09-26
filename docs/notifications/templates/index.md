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

### Notification Report

Enables the session report as the notification template data, including container statuses and logs.

```text
            Argument: --notification-report
Environment Variable: WATCHTOWER_NOTIFICATION_REPORT
                Type: Boolean
             Default: false
```

The template is a [Go template](https://golang.org/pkg/text/template/){target="_blank" rel="noopener noreferrer"} that processes either a list of [Logrus](https://pkg.go.dev/github.com/sirupsen/logrus?tab=doc#Entry){target="_blank" rel="noopener noreferrer"} log entries or a `notifications.Data` struct, depending on the `--notification-report` flag.

## Simple Templates

Simple templates are used when `--notification-report` is not set, formatting individual log entries as they occur.

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
{{- else if $e.Data -}}
    {{$msg}} | {{range $k, $v := $e.Data -}}{{$k}}={{$v}} {{- end}}
{{- else -}}
    {{$msg}}
{{- end -}}
{{- end -}}
```

- This template processes `info`-level log entries in real-time, formatting key update events in past tense with container and image details from `logrus` fields.
- It sends each event immediately in legacy mode, mimicking a step-by-step log.

### Using Simple Templates in the Preview Tool

The [Template Preview Tool](../template-preview/index.md) uses a `notifications.Data` struct with `.Entries` as the log list.

!!! Note
    To use the simple template in the preview tool, modify the range to `{{- range $i, $e := .Entries -}}` to match the data structure.

```go title="Example Simple Template for the Template Preview Tool"
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

When `--notification-report` is set, the template processes a `notifications.Data` struct containing a session report and log entries.

```go title="Default Report Template"
{{- if .Report -}}
  {{- with .Report -}}
    {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Failed}} Failed
    {{- if ( or .Updated .Failed ) -}}
      {{- range .Updated}}
- {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}
      {{- end -}}
      {{- range .Fresh}}
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
```

- This template generates a summary of container statuses (scanned, updated, failed, etc.) followed by logs, used for notifications like email or Slack messages.

### Example Usage
<!-- markdownlint-disable -->
=== "Docker CLI"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATION_REPORT="true" \
      -e WATCHTOWER_NOTIFICATION_TEMPLATE="\
    {{- if .Report -}}
      {{- with .Report -}}
    {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Failed}} Failed
    {{- if ( or .Updated .Failed ) -}}
          {{- range .Updated}}
    - {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}
          {{- end -}}
          {{- range .Fresh}}
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
    {{range .Entries -}}{{.Time.Format \"2006-01-02T15:04:05Z07:00\"}} [{{.Level}}] {{.Message}}{{\"\n\"}}{{- end -}}
    " \
      watchtower-image
    ```

=== "Docker Compose"

    ```yaml
    services:
      watchtower:
        image: watchtower-image
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
            {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Failed}} Failed
            {{- if ( or .Updated .Failed ) -}}
                {{- range .Updated}}
            - {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}
                {{- end -}}
                {{- range .Fresh}}
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
    ```
<!-- markdownlint-restore -->
Example output for a session with one updated container and one error log:

```text
5 Scanned, 1 Updated, 0 Failed
- /container (repo/image:latest): abcdef12 updated to 34567890

Logs:
2025-08-20T06:00:13-07:00 [error] Operation failed. Try again later.
```

## Customizing Templates

You can create custom templates to format notifications differently.

Use the [Template Preview Tool](../template-preview/index.md) to test your templates interactively.

!!! Note
    When testing simple templates in the preview tool, ensure the range iterates over `.Entries` (e.g., `{{- range $i, $e := .Entries -}}`) to match the `notifications.Data` struct.

## Additional Resources

- For detailed template syntax, refer to the [Go Template documentation](https://golang.org/pkg/text/template/){target="_blank" rel="noopener noreferrer"}.
- For log entry fields, see the [Logrus Entry documentation](https://pkg.go.dev/github.com/sirupsen/logrus?tab=doc#Entry){target="_blank" rel="noopener noreferrer"}.
