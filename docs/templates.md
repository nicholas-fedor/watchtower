# Templates

You can customize the message posted by setting a template.

- `--notification-template` (env. `WATCHTOWER_NOTIFICATION_TEMPLATE`): The template used for the message.

The template is a Go [template](https://golang.org/pkg/text/template/) that either format a list
of [log entries](https://pkg.go.dev/github.com/sirupsen/logrus?tab=doc#Entry) or a `notification.Data` struct.

Simple templates are used unless the `notification-report` flag is specified:

- `--notification-report` (env. `WATCHTOWER_NOTIFICATION_REPORT`): Use the session report as the notification template data.

## Simple templates

The default template for simple notifications (used when `WATCHTOWER_NOTIFICATION_REPORT` is not set) is:

```go
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

This template processes `info`-level log entries as they occur, formatting key update events in past tense with container and image details from `logrus` fields. It sends each event immediately in legacy mode, mimicking a step-by-step log.

Example output for a single container update with `WATCHTOWER_CLEANUP` enabled:

```text
Found new image: /app:latest (bb7ba9626731)
Stopped stale container: /app (4a2a8f7298a2)
Started new container: /app (f52721881bed)
Removed stale image: 78612560eb20
```

!!! note "Field Handling"
    If expected fields (e.g., `new_id`, `id`, `image_id`) are missing, the template uses "unknown" as a fallback to ensure readable output (e.g., `Stopped stale container: /app (unknown)`).

!!! tip "Custom date format"
    To include timestamps, modify the template with .Time.Format, e.g., {{.Time.Format "2006-01-02 15:04:05"}} {{$msg}}. The reference time format is Mon Jan 2 15:04:05 MST 2006, so adjust accordingly (e.g., day as 1, month as 2, hour as 3 or 15).

!!! note "Skipping notifications"
    To skip sending notifications that do not contain any information, you can wrap your template with `{{if .}}` and `{{end}}`. The default template does not skip empty messages in legacy mode, as it processes logs as they occur.

Example:

```bash
docker run -d \
  --name watchtower \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -e WATCHTOWER_NOTIFICATION_URL="discord://token@channel slack://watchtower@token-a/token-b/token-c" \
  -e WATCHTOWER_NOTIFICATION_TEMPLATE="{{range .}}{{.Time.Format \"2006-01-02 15:04:05\"}} ({{.Level}}): {{.Message}}{{println}}{{end}}" \
  nickfedor/watchtower
```

## Report templates

The default template for report notifications are the following:

```go
{{- if .Report -}}
  {{- with .Report -}}
    {{- if ( or .Updated .Failed ) -}}
{{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Failed}} Failed
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
{{- else -}}
  {{range .Entries -}}{{.Message}}{{"\n"}}{{- end -}}
{{- end -}}
```

It will be used to send a summary of every session if there are any containers that were updated or which failed to update.
<!-- markdownlint-disable -->
!!! note "Skipping notifications"
    Whenever the result of applying the template results in an empty string, no notifications will
    be sent. This is by default used to limit the notifications to only be sent when there something noteworthy occurred.

    You can replace `{{- if ( or .Updated .Failed ) -}}` with any logic you want to decide when to send the notifications.
<!-- markdownlint-restore -->
Example using a custom report template that always sends a session report after each run:
<!-- markdownlint-disable -->
=== "docker run"

    ```bash
    docker run -d \
      --name watchtower \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e WATCHTOWER_NOTIFICATION_REPORT="true" \
      -e WATCHTOWER_NOTIFICATION_URL="discord://token@channel slack://watchtower@token-a/token-b/token-c" \
      -e WATCHTOWER_NOTIFICATION_TEMPLATE="
      {{- if .Report -}}
        {{- with .Report -}}
      {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Failed}} Failed
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
      {{- else -}}
        {{range .Entries -}}{{.Message}}{{\"\n\"}}{{- end -}}
      {{- end -}}
      " \
      nickfedor/watchtower
    ```

=== "docker-compose"

    ``` yaml
    version: "3"
    services:
      watchtower:
        image: nickfedor/watchtower
        volumes:
          - /var/run/docker.sock:/var/run/docker.sock
        env:
          WATCHTOWER_NOTIFICATION_REPORT: "true"
          WATCHTOWER_NOTIFICATION_URL: >
            discord://token@channel
            slack://watchtower@token-a/token-b/token-c
          WATCHTOWER_NOTIFICATION_TEMPLATE: |
            {{- if .Report -}}
              {{- with .Report -}}
            {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Failed}} Failed
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
            {{- else -}}
              {{range .Entries -}}{{.Message}}{{"\n"}}{{- end -}}
            {{- end -}}
    ```
<!-- markdownlint-restore -->
