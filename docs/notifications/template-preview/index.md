---
hide:
  - toc
---

# Template Preview

<link rel="stylesheet" href="styles.css">
<script src="../../assets/wasm_exec.js"></script>
<script src="script.js"></script>

<form id="tplprev" onchange="formChanged(event)" onsubmit="formSubmitted(event)">
<pre class="loading">loading wasm...</pre>
<div class="template-wrapper">
<textarea name="template" type="text" onkeyup="inputUpdated()">{{- with .Report -}}
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
</div>
<div class="controls">
<fieldset>
    <input type="hidden" name="report" value="yes" />
    <legend><label><input type="checkbox" data-toggle="report" checked /> Container report</label></legend>
    <label class="numfield">
        Skipped:
        <input type="number" name="skipped" value="3" />
    </label>
    <label class="numfield">
        Scanned:
        <input type="number" name="scanned" value="3" />
    </label>
    <label class="numfield">
        Updated:
        <input type="number" name="updated" value="3" />
    </label>
    <label class="numfield">
        Failed:
        <input type="number" name="failed" value="3" />
    </label>
    <label class="numfield">
        Fresh:
        <input type="number" name="fresh" value="3" />
    </label>
    <label class="numfield">
        Stale:
        <input type="number" name="stale" value="3" />
    </label>
</fieldset>
<fieldset>
    <input type="hidden" name="log" value="yes" />
    <legend><label><input type="checkbox" data-toggle="log" checked /> Log entries</label></legend>
    <label class="numfield">
        Error:
        <input type="number" name="error" value="1" />
    </label>
    <label class="numfield">
        Warning:
        <input type="number" name="warning" value="2" />
    </label>
    <label class="numfield">
        Info:
        <input type="number" name="info" value="3" />
    </label>
    <label class="numfield">
        Debug:
        <input type="number" name="debug" value="4" />
    </label>
</fieldset>
<button type="submit">Update preview</button>
</div>
<div class="result-wrapper">
    <pre id="result"></pre>
</div>
</form>
