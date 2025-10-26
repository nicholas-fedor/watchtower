// Package notifications provides notification templates and utilities for Watchtower container updates.
// It defines common templates for formatting update reports and event logs in various output formats.
package notifications

// commonTemplates defines a map of predefined notification templates for different output formats.
// Each template is a Go template string that formats container update information.
// Templates can access either .Report (summary data with container lists) or .Entries (individual event logs).
// Container states include: Updated (successfully updated), Failed (update failed), Fresh (no update needed), Skipped (skipped for various reasons).
var commonTemplates = map[string]string{
	// "default-legacy" template formats individual event entries in a legacy log style.
	// It iterates over .Entries, checking each entry's Message to format specific container lifecycle events.
	// Handles messages: "Found new image" (new image available), "Stopping container" (stopping old container),
	// "Started new container" (new container started), "Removing image" (old image removed), "Container updated" (update completed).
	// For unrecognized messages, displays the message with key=value data pairs if Data exists, otherwise just the message.
	// Expects .Entries []Entry where each Entry has Message string and Data map[string]interface{}.
	"default-legacy": `
{{- /* Iterate over entries, adding newline between them */ -}}
{{- range $i, $e := . -}}
{{- /* Add newline if not the first entry */ -}}
{{- if $i}}{{- println -}}{{- end -}}
{{- /* Extract message for conditional formatting */ -}}
{{- $msg := $e.Message -}}
{{- /* Format based on specific message types */ -}}
{{- if eq $msg "Found new image" -}}
    Found new image: {{$e.Data.image}} ({{with $e.Data.new_id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Stopping container" -}}
    Stopped stale container: {{$e.Data.container}} ({{with $e.Data.id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Started new container" -}}
    Started new container: {{$e.Data.container}} ({{with $e.Data.new_id}}{{.}}{{else}}unknown{{end}})
{{- else if eq $msg "Removing image" -}}
    Removed stale image: {{with $e.Data.image_id}}{{.}}{{else}}unknown{{end}}
{{- else if eq $msg "Container updated" -}}
    Updated container: {{with $e.Data.container}}{{.}}{{else}}unknown{{end}} ({{with $e.Data.image}}{{.}}{{else}}unknown{{end}}): {{with $e.Data.old_id}}{{.}}{{else}}unknown{{end}} updated to {{with $e.Data.new_id}}{{.}}{{else}}unknown{{end}}
{{- else if $e.Data -}}
    {{- /* For messages with data, show message and key=value pairs */ -}}
    {{$msg}} | {{range $k, $v := $e.Data -}}{{$k}}={{$v}} {{- end}}
{{- else -}}
    {{- /* For messages without data, show just the message */ -}}
    {{$msg}}
{{- end -}}
{{- end -}}`,

	// "default" template provides a human-readable summary report of container update operations.
	// If .Report exists, displays counts of scanned/updated/failed containers, then lists details for each category.
	// Updated containers show name, image, and old/new image IDs.
	// Fresh containers (no update needed) show name, image, and state.
	// Skipped containers show name, image, state, and error reason.
	// Failed containers show name, image, state, and error details.
	// If no .Report, falls back to listing all .Entries messages (one per line).
	// Expects .Report with Scanned, Updated, Failed, Fresh, Skipped slices of containers.
	// Each container has Name, ImageName, State, Error, CurrentImageID, LatestImageID fields.
	`default`: `
{{- if .Report -}}
  {{- /* Use report summary data */ -}}
  {{- with .Report -}}
    {{len .Scanned}} Scanned, {{len .Updated}} Updated, {{len .Failed}} Failed
      {{- /* List successfully updated containers */ -}}
      {{- range .Updated}}
- {{.Name}} ({{.ImageName}}): {{.CurrentImageID.ShortID}} updated to {{.LatestImageID.ShortID}}
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

	// "porcelain.v1.summary-no-log" template provides machine-readable output for scripting/automation.
	// Iterates over all containers in .Report.All, showing name, image, state, and error (if any) per line.
	// If no containers matched the filter, outputs "no containers matched filter".
	// Expects .Report.All []Container slice with Name, ImageName, State, Error fields.
	`porcelain.v1.summary-no-log`: `
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

	// "json.v1" template outputs the entire data structure as JSON for programmatic consumption.
	// Useful for integrations that need structured data.
	// Expects any data structure that can be JSON marshaled (typically the full report or entries).
	`json.v1`: `{{ . | ToJSON }}`,
}
