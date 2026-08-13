package templates

import (
	"maps"
	"slices"
)

// Templates is a snapshot of Watchtower's named notification templates.
var Templates = map[string]string{
	"default-legacy": `
{{- /* Iterate over entries, adding newline between them */ -}}
{{- range $i, $e := . -}}
{{- /* Add newline if not the first entry */ -}}
{{- if $i}}{{- println -}}{{- end -}}
{{- /* Extract message for conditional formatting */ -}}
{{- $msg := $e.Message -}}
{{- /* Format based on specific message types */ -}}
{{- if eq $msg "Found new image" -}}
    Found new image: {{index $e.Data "image"}} ({{with (index $e.Data "new_id")}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Stopping container" -}}
    Stopped stale container: {{index $e.Data "container"}} ({{with (index $e.Data "id")}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Started new container" -}}
    Started new container: {{index $e.Data "container"}} ({{with (index $e.Data "new_id")}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Stopping linked container" -}}
    Stopped linked container: {{index $e.Data "container"}} ({{with (index $e.Data "id")}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Started linked container" -}}
    Started linked container: {{index $e.Data "container"}} ({{with (index $e.Data "new_id")}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Removing image" -}}
    Cleaned up old image: {{with (index $e.Data "image_name")}}{{.}}{{else}}unknown{{end}} ({{with (index $e.Data "image_id")}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Container updated" -}}
    Updated container: {{with (index $e.Data "container")}}{{.}}{{else}}unknown{{end}} ({{with (index $e.Data "image")}}{{.}}{{else}}unknown{{end}}): {{with (index $e.Data "old_id")}}{{.}}{{else}}unknown{{end}} updated to {{with (index $e.Data "new_id")}}{{.}}{{else}}unknown{{end}}
{{- else if eq $msg "Skipping Watchtower self-update in run-once mode" -}}
    Run once mode: Watchtower self-update skipped
{{- else if eq $msg "Detected multiple Watchtower instances - initiating cleanup" -}}
    Detected {{index $e.Data "count"}} Watchtower instances - initiating cleanup
	{{- else if eq $msg "Successfully removed all excess Watchtower containers" -}}
    Successfully removed {{index $e.Data "removed_instances"}} old Watchtower container{{if ne (index $e.Data "removed_instances") 1}}s{{end}}
{{- else if eq $msg "Image is within cooldown period - not eligible for update" -}}
    {{with (index $e.Data "image")}}{{.}}{{else}}unknown{{end}} created less than {{with (index $e.Data "cooldown")}}{{.}}{{else}}unknown{{end}} ago - eligible in {{with (index $e.Data "eligible_in")}}{{.}}{{else}}unknown{{end}} ({{with (index $e.Data "eligible_at")}}{{RFC1123 .}}{{else}}unknown{{end}})
{{- else if eq $msg "Image age exceeds cooldown - eligible for update" -}}
    {{with (index $e.Data "image")}}{{.}}{{else}}unknown{{end}} created more than {{with (index $e.Data "cooldown")}}{{.}}{{else}}unknown{{end}} ago - eligible for update
{{- else if eq $msg "Image creation time unavailable - update check unavailable" -}}
    {{with (index $e.Data "image")}}{{.}}{{else}}unknown{{end}} creation time unavailable (cooldown: {{with (index $e.Data "cooldown")}}{{.}}{{else}}unknown{{end}}) - update check unavailable
{{- else if eq $msg "Starting HTTP API server" -}}
    Starting HTTP API server at {{if (index $e.Data "tls")}}https{{else}}http{{end}}://{{index $e.Data "host"}}:{{index $e.Data "port"}}
{{- else if eq $msg "HTTP API server is enabled" -}}
    HTTP API server is available at {{if (index $e.Data "tls")}}https{{else}}http{{end}}://{{index $e.Data "host"}}:{{index $e.Data "port"}}
{{- else if eq $msg "Only checking containers in scope" -}}
    Only checking containers in scope: {{index $e.Data "scope"}}
{{- else if $e.Data -}}
    {{- /* For messages with data, show message and key=value pairs */ -}}
    {{$msg}} | {{range $k, $v := $e.Data}}{{$k}}={{$v}} {{end}}
{{- else -}}
    {{- /* For messages without data, show just the message */ -}}
    {{$msg}}
{{- end -}}
{{- end -}}`,

	"default": `
{{- if .Report -}}
  {{- /* Use report summary data */ -}}
  {{- with .Report -}}
    {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Restarted}} Restarted, {{len .Failed}} Failed, {{len .Fresh}} Fresh, {{len .Skipped}} Skipped
      {{- /* List successfully updated containers */ -}}
      {{- range .Updated}}
- {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}
      {{- end -}}
      {{- /* List restarted containers */ -}}
      {{- range .Restarted}}
- {{.Name}} ({{.ImageName}}): {{.State}}
      {{- end -}}
      {{- /* List fresh containers (no update needed) */ -}}
      {{- range .Fresh}}
- {{.Name}} ({{.ImageName}}): {{.State}}
	  {{- end -}}
	  {{- /* List skipped containers with reason */ -}}
	  {{- range .Skipped}}
- {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
	  {{- end -}}
	  {{- /* List failed containers with error */ -}}
	  {{- range .Failed}}
- {{.Name}} ({{.ImageName}}): {{.State}}: {{.Error}}
	  {{- end -}}
  {{- end -}}
{{- else -}}
  {{- /* Fallback to simple entry messages */ -}}
  {{range .Entries -}}{{.Message}}{{"\n"}}{{- end -}}
{{- end -}}`,

	"porcelain.v1.summary-no-log": `
{{- if .Report -}}
  {{- /* Iterate over all containers */ -}}
  {{- range .Report.All }}
    {{- .Name}} ({{.ImageName}}): {{.State -}}
    {{- with .Error}} Error: {{.}}{{end}}{{ println }}
  {{- else -}}
    no containers matched filter
  {{- end -}}
{{- else -}}
  no containers matched filter
{{- end -}}`,

	"json.v1": `{{ . | ToJSON }}`,

	"porcelain.json": `{{ .Report | ToPorcelainJSON }}`,
}

// Lookup returns a named builtin template.
//
// Parameters:
//   - name: Builtin template name.
//
// Returns:
//   - string: Template source when found.
//   - bool: True when name is a known builtin.
func Lookup(name string) (string, bool) {
	tpl, found := Templates[name]

	return tpl, found
}

// Names returns the builtin template names in sorted order.
//
// Returns:
//   - []string: Sorted builtin names.
func Names() []string {
	return slices.Sorted(maps.Keys(Templates))
}
