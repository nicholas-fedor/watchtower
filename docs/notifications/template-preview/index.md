---
hide:
  - toc
---

# Template Preview
<!-- Page title for the template preview tool -->

<link rel="stylesheet" href="styles.css">
<!-- Link to the custom CSS stylesheet for styling the form and preview -->

<script src="../../assets/wasm_exec.js"></script>
<!-- Script for WebAssembly runtime support -->

<script src="script.js"></script>
<!-- Script containing JavaScript logic for form interactions, WASM loading, and preview updates -->

<form id="tplprev" role="form">
<!-- Form element for user input and preview generation, with ID for JS targeting and role for accessibility -->

<pre class="loading">loading wasm...</pre>
<!-- Loading indicator shown while WebAssembly module is loading -->

<div class="template-wrapper">
<!-- Wrapper div for the template textarea, used for styling and layout -->

<textarea name="template">{{- with .Report -}}
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
{{- if (and .Entries .Report) }}

Logs:
{{ end -}}
{{range .Entries -}}{{.Time.Format "2006-01-02T15:04:05Z07:00"}} [{{.Level}}] {{.Message}}{{"\n"}}{{- end -}}</textarea>
<!-- Textarea for user to edit the notification template, with default Go template content for report and logs -->

</div>
<!-- Closing div for template-wrapper -->

<div class="controls">
<!-- Wrapper div for control fieldsets, used for styling and layout -->

<fieldset id="report-fieldset">
<!-- Fieldset for container report controls, with ID for potential JS targeting -->

    <input type="hidden" name="report" value="yes" />
    <!-- Hidden input to toggle report generation, default to "yes" -->

    <legend><label><input type="checkbox" data-toggle="report" checked /> Container report</label></legend>
    <!-- Legend with checkbox to toggle report visibility, checked by default, with data-toggle attribute for JS handling -->

    <label class="numfield">
        Skipped:
        <input type="number" name="skipped" value="3" min="0" />
    </label>
    <!-- Label and number input for skipped containers count, default value 3, minimum value 0 -->

    <label class="numfield">
        Scanned:
        <input type="number" name="scanned" value="3" min="0" />
    </label>
    <!-- Label and number input for scanned containers count, default value 3, minimum value 0 -->

    <label class="numfield">
        Updated:
        <input type="number" name="updated" value="3" min="0" />
    </label>
    <!-- Label and number input for updated containers count, default value 3, minimum value 0 -->

    <label class="numfield">
        Failed:
        <input type="number" name="failed" value="3" min="0" />
    </label>
    <!-- Label and number input for failed containers count, default value 3, minimum value 0 -->

    <label class="numfield">
        Fresh:
        <input type="number" name="fresh" value="3" min="0" />
    </label>
    <!-- Label and number input for fresh containers count, default value 3, minimum value 0 -->

    <label class="numfield">
        Stale:
        <input type="number" name="stale" value="3" min="0" />
    </label>
    <!-- Label and number input for stale containers count, default value 3, minimum value 0 -->

</fieldset>
<!-- Closing fieldset for report controls -->

<fieldset id="log-fieldset">
<!-- Fieldset for log entries controls, with ID for potential JS targeting -->

    <input type="hidden" name="log" value="yes" />
    <!-- Hidden input to toggle log generation, default to "yes" -->

    <legend><label><input type="checkbox" data-toggle="log" checked /> Log entries</label></legend>
    <!-- Legend with checkbox to toggle log visibility, checked by default, with data-toggle attribute for JS handling -->

    <label class="numfield">
        Error:
        <input type="number" name="error" value="1" min="0" />
    </label>
    <!-- Label and number input for error log count, default value 1, minimum value 0 -->

    <label class="numfield">
        Warning:
        <input type="number" name="warning" value="2" min="0" />
    </label>
    <!-- Label and number input for warning log count, default value 2, minimum value 0 -->

    <label class="numfield">
        Info:
        <input type="number" name="info" value="3" min="0" />
    </label>
    <!-- Label and number input for info log count, default value 3, minimum value 0 -->

    <label class="numfield">
        Debug:
        <input type="number" name="debug" value="4" min="0" />
    </label>
    <!-- Label and number input for debug log count, default value 4, minimum value 0 -->

</fieldset>
<!-- Closing fieldset for log controls -->

</div>
<!-- Closing div for controls -->

<div class="result-wrapper">
<!-- Wrapper div for the preview result area, used for styling and layout -->

    <div id="result"></div>
    <!-- Div for rendering the template preview output, with ID for JS targeting -->

</div>
<!-- Closing div for result-wrapper -->

</form>
<!-- Closing form element -->
