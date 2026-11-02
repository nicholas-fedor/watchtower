// Package format renders notification messages from the built-in templates or
// a user's own template. A template receives the queued log entries and the
// session report. The built-in JSON and porcelain templates render the report
// in machine-readable forms.
//
// Users write their own templates against the template data and its field
// names. Both are part of Watchtower's user-facing surface, as is the output of
// the built-in templates.
package format
